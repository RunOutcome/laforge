// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package forge

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
}

type entry struct {
	session *terminal.Session
	profile string
	title   string
	cwd     string
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
	spec := terminal.LaunchSpec{
		Command:    p.Command,
		Args:       p.Args,
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
	e := &entry{session: session, profile: p.Name, title: title, cwd: cwd}
	s.mu.Lock()
	s.byID[id] = e
	s.mu.Unlock()
	if s.changed != nil {
		s.changed()
	}
	return e.pane(), nil
}

func (e *entry) pane() Pane {
	return Pane{SessionView: e.session.View(), Profile: e.profile, Title: e.title, Cwd: e.cwd}
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
		e.title = t
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
