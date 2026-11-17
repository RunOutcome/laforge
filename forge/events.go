// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package forge

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/RunOutcome/laforge/terminal"
)

// Event is one change the forge tells its followers about.
type Event struct {
	// Seq numbers events from 1 in the order the forge made them.
	Seq int64 `json:"seq"`
	// Type is pane.changed (Pane is the pane now) or pane.closed (it is
	// gone; Pane is its last view).
	Type string    `json:"type"`
	At   time.Time `json:"at"`
	Pane Pane      `json:"pane"`
}

// hub fans events out to followers. A follower that falls a full buffer
// behind is let go (its channel closes) and resubscribes for a fresh list,
// as a terminal viewer does.
type hub struct {
	mu        sync.Mutex
	seq       int64
	followers map[int]chan Event
	next      int
}

func newHub() *hub { return &hub{followers: map[int]chan Event{}} }

func (h *hub) publish(typ string, p Pane) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	ev := Event{Seq: h.seq, Type: typ, At: time.Now().UTC(), Pane: p}
	for id, ch := range h.followers {
		select {
		case ch <- ev:
		default:
			close(ch)
			delete(h.followers, id)
		}
	}
}

func (h *hub) follow() (<-chan Event, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.next++
	id := h.next
	ch := make(chan Event, 256)
	h.followers[id] = ch
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if c, ok := h.followers[id]; ok {
			close(c)
			delete(h.followers, id)
		}
	}
}

// changed is the sessions' notice that pane id changed (or is gone).
func (s *Server) changed(id string) {
	if e, ok := s.sessions.get(id); ok {
		p := e.pane()
		s.mu.Lock()
		s.last[id] = p
		s.mu.Unlock()
		s.events.publish("pane.changed", p)
		return
	}
	s.mu.Lock()
	p, had := s.last[id]
	delete(s.last, id)
	s.mu.Unlock()
	if had {
		s.events.publish("pane.closed", p)
	}
}

// events streams changes as server-sent events: first a "panes" event with
// every pane, then one event per change. EventSource in a browser (with the
// cookie) and `la events` / `la wait` (with the bearer token) both read it.
func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "events", "This server cannot stream.")
		return
	}
	feed, cancel := s.events.follow()
	defer cancel()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	send := func(name string, v any) error {
		raw, _ := json.Marshal(v)
		_, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, raw)
		flusher.Flush()
		return err
	}
	if send("panes", s.sessions.list()) != nil {
		return
	}
	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case ev, open := <-feed:
			if !open {
				return // fell behind: the client reconnects for a fresh list
			}
			if send(ev.Type, ev) != nil {
				return
			}
		}
	}
}

// reportStatus is POST /api/sessions/{id}/status: a hook (or any program
// holding the token) reports a status record for the pane, with the meaning
// the same report has as the program's own OSC 7501.
func (s *Server) reportStatus(w http.ResponseWriter, r *http.Request) {
	e, ok := s.sessions.get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "no_session", errNoSession.Error())
		return
	}
	var rep terminal.Report
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&rep); err != nil {
		writeError(w, http.StatusBadRequest, "input", "The body is not JSON.")
		return
	}
	if _, err := e.session.Report(rep); err != nil {
		switch {
		case errors.Is(err, terminal.ErrBadReport):
			writeError(w, http.StatusBadRequest, "bad_report", err.Error())
		case errors.Is(err, terminal.ErrSessionClosed):
			writeError(w, http.StatusConflict, "ended", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "report", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, e.pane())
}

// markSeen is POST /api/sessions/{id}/seen: a viewer looked at the pane.
func (s *Server) markSeen(w http.ResponseWriter, r *http.Request) {
	e, ok := s.sessions.get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "no_session", errNoSession.Error())
		return
	}
	before := e.pane().Seen
	e.markSeen()
	if !before {
		s.changed(r.PathValue("id"))
	}
	writeJSON(w, http.StatusOK, e.pane())
}
