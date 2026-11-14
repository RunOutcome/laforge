// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package forge

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RunOutcome/laforge/terminal"
)

// Pane is a running or finished terminal session, as the client sees it.
type Pane struct {
	terminal.SessionView
	Profile string `json:"profile"`
	Title   string `json:"title"`
	Cwd     string `json:"cwd"`
	// After names the agent profile a pane ran before it became a shell.
	After string `json:"after,omitempty"`
}

type entry struct {
	session *terminal.Session
	mu      sync.Mutex
	profile string
	title   string
	cwd     string
	command string // what the pane runs, as the user would type it
	after   string // the agent profile this pane ran before it became a shell
}

// sessions holds every session the forge has started.
type sessions struct {
	mu      sync.RWMutex
	byID    map[string]*entry
	changed func()
}

func newSessions(changed func()) *sessions {
	return &sessions{byID: map[string]*entry{}, changed: changed}
}

// StartRequest is the body of POST /api/sessions.
type StartRequest struct {
	Profile string `json:"profile"`
	Cwd     string `json:"cwd"`
	Title   string `json:"title"`
	Cols    uint16 `json:"cols"`
	Rows    uint16 `json:"rows"`
}

// strippedEnv are variables of the process that started the forge that would
// mislead a child: an agent CLI started from inside another agent's session
// refuses to run, or reports into the wrong one.
var strippedEnv = []string{"CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_SSE_PORT"}

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "s-" + hex.EncodeToString(b)
}

func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}

func (s *sessions) start(p Profile, req StartRequest, defaultCwd string, scrollback int) (Pane, error) {
	cwd := expandHome(strings.TrimSpace(req.Cwd))
	if cwd == "" {
		cwd = defaultCwd
	}
	if info, err := os.Stat(cwd); err != nil || !info.IsDir() {
		return Pane{}, fmt.Errorf("%s is not a folder", cwd)
	}
	id := newID()
	env := append([]string{}, p.Env...)
	env = append(env, "LAFORGE_SESSION="+id, "COLORTERM=truecolor", "TERM_PROGRAM=LaForge")
	command, args := p.Command, p.Args
	display := strings.TrimSpace(p.Command + " " + strings.Join(p.Args, " "))
	if p.Agent && runtime.GOOS != "windows" {
		command, args = thenShell(p)
	}
	spec := terminal.LaunchSpec{
		Command:    command,
		Args:       args,
		Workdir:    cwd,
		Env:        env,
		Columns:    req.Cols,
		Rows:       req.Rows,
		InheritAll: true,
		Unset:      strippedEnv,
	}
	session, err := terminal.Start(id, spec, scrollback, func(*terminal.Session) {
		if s.changed != nil {
			s.changed()
		}
	})
	if err != nil {
		return Pane{}, err
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = p.Label
	}
	e := &entry{session: session, profile: p.Name, title: title, cwd: cwd, command: display}
	if p.Agent && runtime.GOOS != "windows" {
		go s.watchForShell(e)
	}
	s.mu.Lock()
	s.byID[id] = e
	s.mu.Unlock()
	if s.changed != nil {
		s.changed()
	}
	return e.pane(), nil
}

func (e *entry) pane() Pane {
	view := e.session.View()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.command != "" {
		view.Command = e.command
	}
	return Pane{SessionView: view, Profile: e.profile, Title: e.title, Cwd: e.cwd, After: e.after}
}

// shellMarker is the private OSC sequence the agent wrapper prints when the
// agent has exited and the pane becomes a shell. Terminals ignore unknown
// OSC sequences, so it never shows.
const shellMarker = "\x1b]7770;laforge-shell\x07"

// thenShell wraps an agent so that when it exits the pane carries on as the
// user's login shell in the same folder, instead of ending.
func thenShell(p Profile) (string, []string) {
	const script = `label=$1; shift
"$@"
code=$?
printf '\033]7770;laforge-shell\007'
printf '\r\n\033[2m── %s exited (%s). This pane is a shell now; exit closes it. ──\033[0m\r\n\r\n' "$label" "$code"
exec "${SHELL:-/bin/sh}" -l`
	args := append([]string{"-c", script, "laforge", p.Label, p.Command}, p.Args...)
	return "/bin/sh", args
}

// watchForShell reads the pane's output for the wrapper's marker and, when
// it comes, relabels the pane as the shell it now is.
func (s *sessions) watchForShell(e *entry) {
	marker := []byte(shellMarker)
	var tail []byte
	var offset int64
	for {
		w := e.session.WatchFrom(offset)
		chunks := [][]byte{w.Output}
		offset = w.From + int64(len(w.Output))
		for {
			for _, chunk := range chunks {
				buf := append(tail, chunk...)
				if bytes.Contains(buf, marker) {
					w.Cancel()
					e.mu.Lock()
					e.after, e.profile, e.title, e.command = e.profile, "shell", "Shell", os.Getenv("SHELL")+" -l"
					e.mu.Unlock()
					if s.changed != nil {
						s.changed()
					}
					return
				}
				if len(buf) >= len(marker) {
					tail = append([]byte{}, buf[len(buf)-len(marker)+1:]...)
				} else {
					tail = buf
				}
			}
			chunk, open := <-w.Feed
			if !open {
				break
			}
			offset += int64(len(chunk))
			chunks = [][]byte{chunk}
		}
		if w.Ended {
			return
		}
		select {
		case <-e.session.Done():
			return
		default:
		}
	}
}

func (s *sessions) get(id string) (*entry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.byID[id]
	return e, ok
}

func (s *sessions) list() []Pane {
	s.mu.RLock()
	out := make([]Pane, 0, len(s.byID))
	for _, e := range s.byID {
		out = append(out, e.pane())
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out
}

func (s *sessions) rename(id, title string) (Pane, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.byID[id]
	if !ok {
		return Pane{}, errNoSession
	}
	if t := strings.TrimSpace(title); t != "" {
		e.mu.Lock()
		e.title = t
		e.mu.Unlock()
	}
	return e.pane(), nil
}

var errNoSession = errors.New("there is no such session")

// close stops a running session (gracefully, then by force after grace) and
// forgets it once it has ended.
func (s *sessions) close(id string, force bool, grace time.Duration) error {
	e, ok := s.get(id)
	if !ok {
		return errNoSession
	}
	if err := e.session.Stop(force); err != nil {
		return err
	}
	go func() {
		if !e.session.Wait(grace) {
			_ = e.session.Stop(true)
			e.session.Wait(grace)
		}
		s.mu.Lock()
		delete(s.byID, id)
		s.mu.Unlock()
		if s.changed != nil {
			s.changed()
		}
	}()
	return nil
}

// stopAll stops every session and waits up to grace for them to end.
func (s *sessions) stopAll(grace time.Duration) {
	s.mu.RLock()
	all := make([]*entry, 0, len(s.byID))
	for _, e := range s.byID {
		all = append(all, e)
	}
	s.mu.RUnlock()
	for _, e := range all {
		_ = e.session.Stop(false)
	}
	deadline := time.Now().Add(grace)
	for _, e := range all {
		if !e.session.Wait(time.Until(deadline)) {
			_ = e.session.Stop(true)
		}
	}
}
