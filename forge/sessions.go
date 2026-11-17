// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package forge

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
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
	// After names the agent profile a pane ran before it became a shell, and
	// AgentExit that agent's exit code.
	After     string `json:"after,omitempty"`
	AgentExit *int   `json:"agentExit,omitempty"`
	// Seen is set when a viewer has looked at the pane since its status last
	// changed: a done agent that was seen is shown as idle (Herdr's rule).
	Seen bool `json:"seen"`
}

type entry struct {
	session *terminal.Session
	mu      sync.Mutex
	profile string
	title   string
	cwd     string
	command string // what the pane runs, as the user would type it
	after   string // the agent profile this pane ran before it became a shell
	// agentExit is that agent's exit code.
	agentExit *int
	// seenAt is the status time a viewer acknowledged (StatusSince then).
	seenAt *time.Time
	seen   bool // a viewer looked before any status
}

// sessions holds every session the forge has started.
type sessions struct {
	mu      sync.RWMutex
	byID    map[string]*entry
	changed func(id string)
	// home is the forge's folder, given to every pane as LAFORGE_HOME so a
	// program in it (la report, la run) finds this forge.
	home string
}

func newSessions(changed func(id string)) *sessions {
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
// The forge's own token and listen address stay with the forge.
var strippedEnv = []string{"CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_SSE_PORT", "LAFORGE_TOKEN", "LAFORGE_LISTEN"}

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
	if _, ok := terminal.LookupCommand(p.Command); !ok {
		return Pane{}, fmt.Errorf("start %s: the command is not installed on this machine", p.Command)
	}
	id := newID()
	env := []string{"LAFORGE_SESSION=" + id, "COLORTERM=truecolor", "TERM_PROGRAM=LaForge"}
	if s.home != "" {
		env = append(env, "LAFORGE_HOME="+s.home)
	}
	command, args := p.Command, p.Args
	display := strings.TrimSpace(p.Command + " " + strings.Join(p.Args, " "))
	var exits *os.File
	var extra []*os.File
	if wrapsAgents && p.Agent {
		// The agent runs inside a small wrapper that becomes the user's shell
		// when the agent exits. The wrapper reports the exit on a private pipe
		// (descriptor 3), which the agent itself never holds, so nothing the
		// agent prints can fake it. The profile's Env reaches the agent only.
		r, w, err := os.Pipe()
		if err != nil {
			return Pane{}, err
		}
		exits, extra = r, []*os.File{w}
		command, args = thenShell(p)
	} else {
		env = append(append([]string{}, p.Env...), env...)
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
		ExtraFiles: extra,
	}
	session, err := terminal.Start(id, spec, scrollback, func(*terminal.Session) { s.notify(id) })
	for _, f := range extra {
		f.Close() // the child holds its own copy
	}
	if err != nil {
		if exits != nil {
			exits.Close()
		}
		return Pane{}, err
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = p.Label
	}
	e := &entry{session: session, profile: p.Name, title: title, cwd: cwd, command: display}
	if exits != nil {
		go s.watchForShell(e, exits, p.Label)
	}
	s.mu.Lock()
	s.byID[id] = e
	s.mu.Unlock()
	s.notify(id)
	return e.pane(), nil
}

func (s *sessions) notify(id string) {
	if s.changed != nil {
		s.changed(id)
	}
}

// wrapsAgents: an agent that exits leaves a shell. Windows has no sh to
// wrap with, so its agent panes end with the agent.
var wrapsAgents = runtime.GOOS != "windows"

func (e *entry) pane() Pane {
	view := e.session.View()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.command != "" {
		view.Command = e.command
	}
	seen := e.seen && view.Since == nil || e.seenAt != nil && view.Since != nil && !view.Since.After(*e.seenAt)
	return Pane{SessionView: view, Profile: e.profile, Title: e.title, Cwd: e.cwd, After: e.after, AgentExit: e.agentExit, Seen: seen}
}

// markSeen records that a viewer looked at the pane as it is now.
func (e *entry) markSeen() {
	since := e.session.View().Since
	e.mu.Lock()
	defer e.mu.Unlock()
	if since == nil {
		e.seen = true
		return
	}
	t := *since
	e.seenAt = &t
}

// LoginShell is the user's shell: $SHELL, else /bin/sh; on Windows,
// PowerShell 7 (pwsh), else Windows PowerShell, else COMSPEC (cmd.exe).
func LoginShell() string {
	if sh := os.Getenv("SHELL"); sh != "" && runtime.GOOS != "windows" {
		return sh
	}
	if runtime.GOOS != "windows" {
		return "/bin/sh"
	}
	for _, name := range []string{"pwsh.exe", "powershell.exe"} {
		if path, ok := terminal.LookupCommand(name); ok {
			return path
		}
	}
	if comspec := os.Getenv("COMSPEC"); comspec != "" {
		return comspec
	}
	return "cmd.exe"
}

// loginArgs are the arguments that start LoginShell as a login shell.
func loginArgs() []string {
	if runtime.GOOS != "windows" {
		return []string{"-l"}
	}
	name := strings.ToLower(filepath.Base(LoginShell()))
	if strings.HasPrefix(name, "pwsh") || strings.HasPrefix(name, "powershell") {
		return []string{"-NoLogo"}
	}
	return nil // cmd.exe
}

// thenShell wraps an agent so that when it exits the pane carries on as the
// user's login shell in the same folder, instead of ending. Arguments: the
// label, the shell, then env assignments and the agent's command line.
//   - trap : INT keeps a Ctrl-C the agent handled from killing the wrapper
//     when the agent exits (dash re-raises a SIGINT it saw); a caught signal
//     is reset to default in the agent on exec, so the agent's Ctrl-C works.
//   - the agent runs with descriptor 3 closed; the wrapper writes the exit
//     code to it, closes it, then execs the shell.
func thenShell(p Profile) (string, []string) {
	const script = `trap : INT
label=$1; sh=$2; shift 2
env "$@" 3>&-
code=$?
printf '%s\n' "$code" >&3
exec 3>&-
printf '\r\n\033[2m── %s exited (%s). This pane is a shell now; exit closes it. ──\033[0m\r\n\r\n' "$label" "$code"
trap - INT
exec "$sh" -l`
	args := []string{"-c", script, "laforge", p.Label, LoginShell()}
	args = append(args, p.Env...)
	args = append(args, p.Command)
	args = append(args, p.Args...)
	return "/bin/sh", args
}

// watchForShell waits for the wrapper's report that the agent exited, then
// relabels the pane as the shell it now is. The pane keeps a name the user
// gave it; only the profile's own label is replaced.
func (s *sessions) watchForShell(e *entry, exits *os.File, label string) {
	defer exits.Close()
	line, err := bufio.NewReader(exits).ReadString('\n')
	if err != nil {
		return // the pane ended without the agent finishing (stopped, killed)
	}
	code, _ := strconv.Atoi(strings.TrimSpace(line))
	e.mu.Lock()
	e.after, e.agentExit, e.profile, e.command = e.profile, &code, "shell", LoginShell()+" -l"
	if e.title == label {
		e.title = "Shell"
	}
	id := e.session.ID()
	e.mu.Unlock()
	// The agent is gone: what it said about itself is no longer true.
	_, _ = e.session.Report(terminal.Report{Status: terminal.Status{State: "clear"}})
	s.notify(id)
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
	e, ok := s.byID[id]
	s.mu.Unlock()
	if !ok {
		return Pane{}, errNoSession
	}
	if t := strings.TrimSpace(title); t != "" {
		e.mu.Lock()
		e.title = t
		e.mu.Unlock()
		s.notify(id)
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
		s.notify(id)
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
