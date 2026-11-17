// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/RunOutcome/laforge/terminal"
)

// forgeClient talks to the forge this process belongs to: the one in
// LAFORGE_HOME (every pane is given it), by its forge.json.
type forgeClient struct {
	state *State
	http  *http.Client
}

func (c *cmd) client(timeout time.Duration) (*forgeClient, error) {
	s, err := c.readState()
	if err != nil {
		return nil, fmt.Errorf("no forge in %s; start one with %s up", c.home, c.cfg.Prog)
	}
	return &forgeClient{state: s, http: &http.Client{Timeout: timeout}}, nil
}

// do sends a request with the forge's token and decodes a JSON answer into
// out (when not nil). A refusal comes back as the forge's reason.
func (f *forgeClient) do(method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, f.state.URL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+f.state.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := f.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e struct{ Error, Reason string }
		_ = json.NewDecoder(resp.Body).Decode(&e)
		if e.Reason == "" {
			e.Reason = resp.Status
		}
		return &apiError{status: resp.StatusCode, code: e.Error, reason: e.Reason}
	}
	if out != nil && resp.StatusCode != http.StatusNoContent {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

type apiError struct {
	status       int
	code, reason string
}

func (e *apiError) Error() string { return e.reason }

// report is `la report STATE …` (a program saying what it is doing, the
// API's form of OSC 7501) and `la report --hook AGENT` (an agent's hook:
// the event arrives as JSON on stdin). A hook never fails its agent: it
// prints nothing, waits at most a moment, and exits 0 whatever happens.
func (c *cmd) report(args []string) error {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.SetOutput(c.cfg.Stderr)
	session := fs.String("session", os.Getenv("LAFORGE_SESSION"), "the pane (default: this one, $LAFORGE_SESSION)")
	hook := fs.String("hook", "", "read an agent hook's event from stdin (claude, codex)")
	kind := fs.String("kind", "", "for blocked: permission, question or auth")
	msg := fs.String("msg", "", "what it is waiting for, or a line about what it is doing")
	title := fs.String("title", "", "a short title")
	app := fs.String("app", "", "the program's name for itself")
	id := fs.String("id", "", "a child record (\"build\", \"tests/unit\"); empty is the pane itself")
	progress := fs.Int("progress", -1, "0..100 (working or blocked)")
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return usageError{err}
	}
	if *hook != "" {
		c.reportHook(*hook, *session, c.cfg.Stdin)
		return nil
	}
	if len(positional) != 1 {
		return usageError{fmt.Errorf("say a state: idle, working, done, blocked, error or clear")}
	}
	if *session == "" {
		return errors.New("not in a LaForge pane; name one with --session")
	}
	r := terminal.Report{ID: *id, Status: terminal.Status{State: positional[0], Kind: *kind, Msg: *msg, Title: *title, App: *app}}
	if *progress >= 0 {
		r.Progress = progress
	}
	f, err := c.client(3 * time.Second)
	if err != nil {
		return err
	}
	return f.do(http.MethodPost, "/api/sessions/"+*session+"/status", r, nil)
}

// hookEvent is the part of an agent hook's input LaForge reads. Claude Code
// and Codex send the same names.
type hookEvent struct {
	Event            string          `json:"hook_event_name"`
	SessionID        string          `json:"session_id"`
	TranscriptPath   string          `json:"transcript_path"`
	Source           string          `json:"source"`
	ToolName         string          `json:"tool_name"`
	ToolInput        json.RawMessage `json:"tool_input"`
	Message          string          `json:"message"`
	NotificationType string          `json:"notification_type"`
	AgentID          string          `json:"agent_id"`
}

// hookReport is what a hook event means for the pane's status, and whether
// it means anything.
func hookReport(agent string, ev hookEvent) (terminal.Report, bool) {
	r := terminal.Report{Status: terminal.Status{App: agent, Source: "hook"}}
	switch ev.Event {
	case "SessionStart":
		r.State = terminal.StatusIdle
	case "UserPromptSubmit", "PreToolUse", "PostToolUse", "PostToolUseFailure":
		r.State = terminal.StatusWorking
	case "PermissionRequest":
		r.State, r.Kind = terminal.StatusBlocked, "permission"
		r.Msg = permissionLine(ev.ToolName, ev.ToolInput)
	case "Notification":
		switch ev.NotificationType {
		case "permission_prompt":
			r.State, r.Kind, r.Msg = terminal.StatusBlocked, "permission", ev.Message
		case "elicitation_dialog":
			r.State, r.Kind, r.Msg = terminal.StatusBlocked, "question", ev.Message
		default: // idle_prompt and the rest say nothing new
			return r, false
		}
	case "Stop":
		r.State = terminal.StatusDone
	case "Interrupt":
		r.State = terminal.StatusIdle
	case "SessionEnd":
		r.State = "clear"
	default:
		return r, false
	}
	return r, true
}

// permissionLine is a one-line account of what a tool wants to do.
func permissionLine(tool string, input json.RawMessage) string {
	var in map[string]any
	_ = json.Unmarshal(input, &in)
	detail := ""
	for _, key := range []string{"command", "file_path", "path", "url", "pattern", "description"} {
		if v, ok := in[key].(string); ok && v != "" {
			detail = v
			break
		}
	}
	detail = strings.Join(strings.Fields(detail), " ")
	if len(detail) > 200 {
		detail = detail[:200] + "…"
	}
	switch {
	case tool == "" && detail == "":
		return "Wants permission"
	case detail == "":
		return tool
	default:
		return tool + ": " + detail
	}
}

func (c *cmd) reportHook(agent, session string, stdin io.Reader) {
	if session == "" {
		return
	}
	raw, _ := io.ReadAll(io.LimitReader(stdin, 1<<20))
	var ev hookEvent
	if json.Unmarshal(raw, &ev) != nil || ev.AgentID != "" {
		return // not an event, or a sub-agent's (its parent speaks for the pane)
	}
	f, err := c.client(700 * time.Millisecond)
	if err != nil {
		return
	}
	if ev.Event == "SessionStart" && ev.SessionID != "" {
		_ = f.do(http.MethodPost, "/api/sessions/"+session+"/agent", map[string]string{"agent": agent, "sessionId": ev.SessionID, "source": ev.Source}, nil)
	}
	if r, ok := hookReport(agent, ev); ok {
		_ = f.do(http.MethodPost, "/api/sessions/"+session+"/status", r, nil)
	}
}

// parseInterspersed parses flags wherever they are among the arguments
// (`la report blocked --msg "Which file?"`) and returns the rest in order.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
}
