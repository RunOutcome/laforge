// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package forge

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/RunOutcome/laforge/holder"
	"github.com/RunOutcome/laforge/terminal"
)

// record is what the forge keeps of a pane on disk (<home>/panes/<id>.json),
// and gives its holder: enough to adopt the pane again after a restart, or
// to resume it once its program is gone.
type record struct {
	ID           string        `json:"id"`
	Profile      string        `json:"profile"`
	Title        string        `json:"title"`
	Cwd          string        `json:"cwd"`
	Command      string        `json:"command"`
	After        string        `json:"after,omitempty"`
	AgentExit    *int          `json:"agentExit,omitempty"`
	AgentSession *AgentSession `json:"agentSession,omitempty"`
	Wrapped      bool          `json:"wrapped,omitempty"`
	StartedAt    time.Time     `json:"startedAt"`
	// LostAt is when the forge found the pane's program gone.
	LostAt  *time.Time        `json:"lostAt,omitempty"`
	Parent  string            `json:"parent,omitempty"`
	Outlive bool              `json:"outlive,omitempty"`
	Notes   bool              `json:"notes,omitempty"`
	Labels  map[string]string `json:"labels,omitempty"`
}

func (r record) json() json.RawMessage { raw, _ := json.Marshal(r); return raw }

// LostPane is a pane whose program is gone, offered to resume.
type LostPane struct {
	ID           string        `json:"id"`
	Profile      string        `json:"profile"`
	Title        string        `json:"title"`
	Cwd          string        `json:"cwd"`
	AgentSession *AgentSession `json:"agentSession,omitempty"`
	StartedAt    time.Time     `json:"startedAt"`
	LostAt       time.Time     `json:"lostAt"`
	// Resumable is set when starting it again continues the agent's own
	// session (its profile knows how, and its hook reported one).
	Resumable bool `json:"resumable"`
}

// lostFor is how long a lost pane is kept.
const lostFor = 7 * 24 * time.Hour

func (e *entry) record(id string, started time.Time) record {
	e.mu.Lock()
	defer e.mu.Unlock()
	return record{ID: id, Profile: e.profile, Title: e.title, Cwd: e.cwd, Command: e.command, After: e.after, AgentExit: e.agentExit,
		AgentSession: e.agentSession, Wrapped: e.wrapped, StartedAt: started, Parent: e.parent, Outlive: e.outlive, Notes: e.notes, Labels: e.labels}
}

func (s *sessions) recordsDir() string { return filepath.Join(s.home, "panes") }

func (s *sessions) recordPath(id string) string { return filepath.Join(s.recordsDir(), id+".json") }

// save writes the pane's record, and gives it to the pane's holder.
func (s *sessions) save(e *entry) {
	if s.home == "" || e.session == nil {
		return
	}
	r := e.record(e.session.ID(), e.session.View().StartedAt)
	raw := r.json()
	if e.held != nil {
		_ = e.held.SetMeta(raw)
	}
	_ = os.MkdirAll(s.recordsDir(), 0o700)
	writeAtomic(s.recordPath(r.ID), raw)
}

func (s *sessions) forgetRecord(id string) {
	if s.home != "" {
		_ = os.Remove(s.recordPath(id))
	}
}

func writeAtomic(path string, raw []byte) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".rec-*")
	if err != nil {
		return
	}
	_, werr := tmp.Write(raw)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Rename(tmp.Name(), path) != nil {
		os.Remove(tmp.Name())
	}
}

// adopt reads the records a forge before this one left: a pane whose
// holder still answers is taken over as it runs; the rest are lost panes.
func (s *sessions) adopt(scrollback int) {
	if s.home == "" {
		return
	}
	files, _ := filepath.Glob(filepath.Join(s.recordsDir(), "*.json"))
	now := time.Now().UTC()
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var r record
		if json.Unmarshal(raw, &r) != nil || r.ID == "" || filepath.Base(f) != r.ID+".json" {
			continue
		}
		if s.holders != nil && holder.Alive(s.holders.Dir, r.ID) {
			if err := s.adoptHeld(r, scrollback); err == nil {
				continue
			} else {
				log.Printf("forge: could not adopt %s: %v", r.ID, err)
			}
		}
		if r.LostAt == nil {
			r.LostAt = &now
			writeAtomic(f, r.json())
		}
		if now.Sub(*r.LostAt) > lostFor {
			os.Remove(f)
			continue
		}
		s.lost[r.ID] = r
	}
}

func (s *sessions) adoptHeld(r record, scrollback int) error {
	id := r.ID
	client, err := holder.Dial(s.holders.Dir, id, nil)
	if err != nil {
		return err
	}
	// the holder's copy of the record is the newest (the forge gives it every change)
	if meta := client.First().Meta; len(meta) > 0 {
		var held record
		if json.Unmarshal(meta, &held) == nil && held.ID == id {
			r = held
		}
	}
	e := &entry{profile: r.Profile, title: r.Title, cwd: r.Cwd, command: r.Command, after: r.After, agentExit: r.AgentExit,
		agentSession: r.AgentSession, wrapped: r.Wrapped, held: client, parent: r.Parent, outlive: r.Outlive, notes: r.Notes, labels: r.Labels}
	first := client.First().Session
	e.session = terminal.Attach(id, client, terminal.LaunchSpec{Command: r.Command, Columns: first.Columns, Rows: first.Rows}, scrollback,
		func(*terminal.Session) { s.notify(id) })
	e.session.SetStartedAt(first.StartedAt)
	if r.Wrapped && r.After == "" {
		label := r.Title
		if p, ok := s.profile(r.Profile); ok {
			label = p.Label
		}
		go s.watchForShell(e, client.AgentExit(), label)
	}
	s.mu.Lock()
	s.byID[id] = e
	s.mu.Unlock()
	return nil
}

// profile finds a profile by name (built-ins and the user's).
func (s *sessions) profile(name string) (Profile, bool) {
	ps, err := loadProfiles(s.home)
	if err != nil {
		return Profile{}, false
	}
	for _, p := range ps {
		if p.Name == name {
			return p, true
		}
	}
	return Profile{}, false
}

func (s *sessions) lostPanes() []LostPane {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]LostPane, 0, len(s.lost))
	for _, r := range s.lost {
		lp := LostPane{ID: r.ID, Profile: r.Profile, Title: r.Title, Cwd: r.Cwd, AgentSession: r.AgentSession, StartedAt: r.StartedAt, LostAt: *r.LostAt}
		if p, ok := s.profile(r.Profile); ok && len(p.Resume) > 0 && r.AgentSession != nil {
			lp.Resumable = true
		}
		out = append(out, lp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out
}

// takeLost removes a lost pane and returns it.
func (s *sessions) takeLost(id string) (record, bool) {
	s.mu.Lock()
	r, ok := s.lost[id]
	delete(s.lost, id)
	s.mu.Unlock()
	if ok {
		s.forgetRecord(id)
	}
	return r, ok
}

// resumeArgs is the profile's arguments to continue an agent session.
func resumeArgs(p Profile, session string) []string {
	args := append([]string{}, p.Args...)
	for _, a := range p.Resume {
		args = append(args, strings.ReplaceAll(a, "{session}", session))
	}
	return args
}

// setAgentSession is POST /api/sessions/{id}/agent: the agent's hook names
// its own session, so the pane can be resumed after its program is gone.
func (s *Server) setAgentSession(w http.ResponseWriter, r *http.Request) {
	e, ok := s.sessions.get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "no_session", errNoSession.Error())
		return
	}
	var body struct {
		Agent     string `json:"agent"`
		SessionID string `json:"sessionId"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&body); err != nil || body.SessionID == "" || len(body.SessionID) > 200 ||
		strings.ContainsAny(body.SessionID, " \t\n'\"\\$`;&|<>") {
		writeError(w, http.StatusBadRequest, "input", "Give agent and a sessionId (no spaces or shell characters).")
		return
	}
	e.mu.Lock()
	e.agentSession = &AgentSession{Agent: body.Agent, SessionID: body.SessionID}
	e.mu.Unlock()
	s.sessions.save(e)
	s.changed(r.PathValue("id"))
	writeJSON(w, http.StatusOK, e.pane())
}

func (s *Server) listLost(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.sessions.lostPanes())
}

func (s *Server) dropLost(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.sessions.takeLost(r.PathValue("id")); !ok {
		writeError(w, http.StatusNotFound, "no_session", "There is no such lost pane.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
