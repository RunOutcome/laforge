// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package forge

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Agents run agents: a program in a pane (an agent CLI, or `la run` in its
// shell) starts panes of its own, gives them work, reads them and waits for
// them. A pane started from inside another pane is its child: it ends when
// its parent ends, unless started to outlive it, and its parent hears (a
// note typed into the parent's pane) when the child needs attention or ends.

// noteQuiet is how long a pane's viewer must have typed nothing before a
// send or a note is typed into it, and draftHold how long at most a send
// waits for that: a person's half-typed line is never interleaved.
var (
	noteQuiet = 3 * time.Second
	draftHold = 60 * time.Second
)

// markHuman records that a viewer typed into the pane.
func (e *entry) markHuman() {
	e.mu.Lock()
	e.lastHuman = time.Now()
	e.mu.Unlock()
}

// waitQuiet waits until the pane's viewer has been quiet for noteQuiet, or
// draftHold has passed, or the pane ends.
func (e *entry) waitQuiet() {
	deadline := time.Now().Add(draftHold)
	for time.Now().Before(deadline) {
		e.mu.Lock()
		since := time.Since(e.lastHuman)
		e.mu.Unlock()
		if since >= noteQuiet {
			return
		}
		select {
		case <-e.session.Done():
			return
		case <-time.After(noteQuiet - since):
		}
	}
}

// typeInto types text into the pane, then Enter when enter is set, after the
// viewer's quiet pause. Enter is sent on its own a moment later: an agent's
// TUI takes a pasted line and its Enter in one read as a newline in the text.
func (e *entry) typeInto(text string, enter bool) error {
	e.waitQuiet()
	e.sendMu.Lock()
	defer e.sendMu.Unlock()
	if text != "" {
		if err := e.session.Write([]byte(text)); err != nil {
			return err
		}
	}
	if enter {
		time.Sleep(80 * time.Millisecond)
		return e.session.Write([]byte("\r"))
	}
	return nil
}

// sendInput is POST /api/sessions/{id}/input {text, enter}: typed as if by
// the pane's viewer, after any half-typed line of theirs is sent or cleared.
func (s *Server) sendInput(w http.ResponseWriter, r *http.Request) {
	e, ok := s.sessions.get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "no_session", errNoSession.Error())
		return
	}
	var body struct {
		Text  string `json:"text"`
		Enter bool   `json:"enter"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "input", "The body is not JSON.")
		return
	}
	if err := e.typeInto(body.Text, body.Enter); err != nil {
		writeError(w, http.StatusConflict, "ended", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, e.pane())
}

// readOutput is GET /api/sessions/{id}/output?lines=N[&raw=1]: the last N
// lines of what the pane drew, as text (escape sequences removed), or the
// retained bytes as they are with raw=1.
func (s *Server) readOutput(w http.ResponseWriter, r *http.Request) {
	e, ok := s.sessions.get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "no_session", errNoSession.Error())
		return
	}
	watch := e.session.WatchFrom(0)
	watch.Cancel()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Query().Get("raw") == "1" {
		w.Write(watch.Output)
		return
	}
	lines := 100
	if n, err := strconv.Atoi(r.URL.Query().Get("lines")); err == nil && n > 0 {
		lines = min(n, 10000)
	}
	io.WriteString(w, lastLines(plainText(watch.Output), lines))
}

var (
	oscSeq  = regexp.MustCompile(`\x1b\][^\x07\x1b]*(\x07|\x1b\\)`)
	csiSeq  = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	escSeq  = regexp.MustCompile(`\x1b[@-Z\\-_]|\x1b[()][0-9A-Za-z]`)
	ctlSeq  = regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1a\x1c-\x1f\x7f]`)
	cursorH = regexp.MustCompile(`\x1b\[[0-9;]*[Hf]`)
)

// plainText is terminal output as lines of text: escape sequences dropped,
// a carriage return overwriting its line (as a progress bar redraws), and
// a cursor jump taken as a new line. Full-screen programs read roughly.
func plainText(raw []byte) string {
	s := strings.ToValidUTF8(string(raw), "")
	s = oscSeq.ReplaceAllString(s, "")
	s = cursorH.ReplaceAllString(s, "\n")
	s = csiSeq.ReplaceAllString(s, "")
	s = escSeq.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if i := strings.LastIndex(strings.TrimRight(line, "\r"), "\r"); i >= 0 {
			line = line[i+1:]
		}
		line = ctlSeq.ReplaceAllString(strings.TrimRight(line, "\r"), "")
		out = append(out, strings.TrimRight(line, " "))
	}
	return strings.Join(out, "\n")
}

func lastLines(s string, n int) string {
	s = strings.TrimRight(s, "\n")
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n") + "\n"
}

// family keeps parents and children in step: when a pane ends (or its agent
// exits, leaving a shell), its children that do not outlive it are closed;
// when a child turns blocked, error or done, or ends, its parent hears.
func (s *Server) family(id string, p Pane, closed bool) {
	ended := closed || p.EndedAt != nil || p.After != ""
	if ended {
		for _, child := range s.sessions.children(id) {
			child.mu.Lock()
			outlive := child.outlive
			child.mu.Unlock()
			if !outlive {
				_ = s.sessions.close(child.session.ID(), false, 3*time.Second)
			}
		}
	}
	e, ok := s.sessions.get(id)
	var parentID string
	var notes bool
	if ok {
		e.mu.Lock()
		parentID, notes = e.parent, e.notes
		e.mu.Unlock()
	} else if closed {
		parentID, notes = p.Parent, p.Notes
	}
	if parentID == "" || !notes {
		return
	}
	note := ""
	key := ""
	switch {
	case closed || p.EndedAt != nil:
		code := ""
		if p.ExitCode != nil {
			code = fmt.Sprintf(" (exit %d)", *p.ExitCode)
		}
		note, key = fmt.Sprintf("ended%s", code), "ended"
	case p.Status != nil && (p.Status.State == "blocked" || p.Status.State == "error" || p.Status.State == "done"):
		note = p.Status.State
		if p.Status.Msg != "" {
			note += ": " + p.Status.Msg
		}
		key = p.Status.State + "@" + p.Since.String()
	default:
		return
	}
	if ok {
		e.mu.Lock()
		if e.noted == key {
			e.mu.Unlock()
			return
		}
		e.noted = key
		e.mu.Unlock()
	}
	parent, pok := s.sessions.get(parentID)
	if !pok || parent.session.View().EndedAt != nil {
		return
	}
	text := fmt.Sprintf("[laforge] %s %q (%s) is %s. Read it with: la read %s", "child", p.Title, p.ID, note, p.ID)
	go func() { _ = parent.typeInto(text, true) }()
}

func (s *sessions) children(id string) []*entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*entry
	for _, e := range s.byID {
		e.mu.Lock()
		p := e.parent
		e.mu.Unlock()
		if p == id {
			out = append(out, e)
		}
	}
	return out
}
