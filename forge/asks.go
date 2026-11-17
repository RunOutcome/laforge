// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package forge

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/RunOutcome/laforge/terminal"
)

// Asks: an agent's permission request answered from the browser (or a
// phone, or la answer), not only in its terminal. The agent's hook (Claude
// Code's PermissionRequest) asks the forge and waits; a person answers
// Allow or Deny on the pane; the hook gives the agent that decision. If no
// one answers in time the hook returns nothing and the agent asks in its
// terminal as it always does. Only the forge's full token can answer.

// Ask is a question a pane's agent is waiting on.
type Ask struct {
	Msg   string    `json:"msg"`
	Since time.Time `json:"since"`
}

type ask struct {
	Ask
	answer chan string
}

// maxAskWait bounds how long a hook may hold an agent waiting for an answer.
const maxAskWait = 10 * time.Minute

// askPermission is POST /api/sessions/{id}/ask {msg, wait}: wait (seconds)
// for a person's allow or deny. The answer is {"decision": "allow"|"deny"},
// or "" when no one answered in time.
func (s *Server) askPermission(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	e, ok := s.sessions.own(id)
	if !ok {
		writeError(w, http.StatusNotFound, "no_session", errNoSession.Error())
		return
	}
	var body struct {
		Msg  string  `json:"msg"`
		App  string  `json:"app"`
		Wait float64 `json:"wait"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil || body.Wait <= 0 {
		writeError(w, http.StatusBadRequest, "input", "Give msg and wait (seconds).")
		return
	}
	wait := min(time.Duration(body.Wait*float64(time.Second)), maxAskWait)
	a := &ask{Ask: Ask{Msg: body.Msg, Since: time.Now().UTC()}, answer: make(chan string, 1)}
	e.mu.Lock()
	old := e.ask
	e.ask = a
	e.mu.Unlock()
	if old != nil {
		old.answer <- "" // a newer question replaces it
	}
	_, _ = s.sessions.report(e, terminal.Report{Status: terminal.Status{State: terminal.StatusBlocked, Kind: "permission", Msg: body.Msg, App: body.App, Source: "hook"}})
	s.changed(id)
	decision := ""
	select {
	case decision = <-a.answer:
	case <-time.After(wait):
	case <-r.Context().Done():
	case <-e.session.Done():
	}
	e.mu.Lock()
	if e.ask == a {
		e.ask = nil
	}
	e.mu.Unlock()
	if decision != "" {
		// the agent carries on (allowed, or told why not)
		_, _ = s.sessions.report(e, terminal.Report{Status: terminal.Status{State: terminal.StatusWorking, App: body.App, Source: "hook"}})
	}
	s.changed(id)
	writeJSON(w, http.StatusOK, map[string]string{"decision": decision})
}

// answerAsk is POST /api/sessions/{id}/answer {decision}: allow or deny
// what the pane's agent is waiting on.
func (s *Server) answerAsk(w http.ResponseWriter, r *http.Request) {
	e, ok := s.sessions.get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "no_session", errNoSession.Error())
		return
	}
	var body struct {
		Decision string `json:"decision"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&body); err != nil || body.Decision != "allow" && body.Decision != "deny" {
		writeError(w, http.StatusBadRequest, "input", "decision is allow or deny.")
		return
	}
	e.mu.Lock()
	a := e.ask
	e.ask = nil
	e.mu.Unlock()
	if a == nil {
		writeError(w, http.StatusConflict, "not_asking", "The pane's agent is not waiting for an answer (it may have asked in its terminal).")
		return
	}
	a.answer <- body.Decision
	writeJSON(w, http.StatusOK, map[string]string{"decision": body.Decision})
}

func (e *entry) asking() *Ask {
	if e.ask == nil {
		return nil
	}
	a := e.ask.Ask
	return &a
}
