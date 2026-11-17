// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package forge

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Shares: a read-only link to one pane. Its token opens that pane's view and
// output and nothing else: no input, no other pane, no change of any kind.
// It expires, and can be revoked. The forge still listens where it listens
// (127.0.0.1 unless told otherwise), so a share reaches whoever can reach
// the forge: on this machine, or through a tunnel the user set up.

// Share is one read-only link.
type Share struct {
	Token   string    `json:"token"`
	Pane    string    `json:"pane"`
	Created time.Time `json:"created"`
	Expires time.Time `json:"expires"`
}

const viewCookie = "laforge_view"

// maxShareFor bounds a share's life.
const maxShareFor = 7 * 24 * time.Hour

type shares struct {
	mu   sync.Mutex
	path string
	all  map[string]Share
}

func newShares(home string) *shares {
	s := &shares{path: filepath.Join(home, "shares.json"), all: map[string]Share{}}
	if raw, err := os.ReadFile(s.path); err == nil {
		var list []Share
		if json.Unmarshal(raw, &list) == nil {
			for _, sh := range list {
				s.all[sh.Token] = sh
			}
		}
	}
	return s
}

func (s *shares) saveLocked() {
	list := make([]Share, 0, len(s.all))
	now := time.Now()
	for k, sh := range s.all {
		if now.After(sh.Expires) {
			delete(s.all, k)
			continue
		}
		list = append(list, sh)
	}
	raw, _ := json.MarshalIndent(list, "", "  ")
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, s.path)
	}
}

// pane is the pane a live share token opens.
func (s *shares) pane(token string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, sh := range s.all {
		if subtle.ConstantTimeCompare([]byte(k), []byte(token)) == 1 && time.Now().Before(sh.Expires) {
			return sh.Pane, true
		}
	}
	return "", false
}

func (s *shares) dropPane(pane string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for k, sh := range s.all {
		if sh.Pane == pane {
			delete(s.all, k)
			changed = true
		}
	}
	if changed {
		s.saveLocked()
	}
}

// viewer is the pane a request's share token opens, if it carries one (and
// no full token).
func (s *Server) viewer(r *http.Request) (string, bool) {
	if c, err := r.Cookie(viewCookie); err == nil {
		return s.shares.pane(c.Value)
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "View ") {
		return s.shares.pane(strings.TrimPrefix(h, "View "))
	}
	return "", false
}

// viewOnly serves a read the share's pane allows: its view, its stream
// (input ignored) and its output. Anything else is refused.
func (s *Server) viewOnly(w http.ResponseWriter, r *http.Request, pane string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusForbidden, "read_only", "This link can only watch.")
		return
	}
	switch {
	case r.URL.Path == "/api/whoami":
		writeJSON(w, http.StatusOK, map[string]any{"mode": "view", "pane": pane})
	case r.URL.Path == "/api/sessions":
		var out []Pane
		if e, ok := s.sessions.get(pane); ok {
			out = append(out, e.pane())
		}
		writeJSON(w, http.StatusOK, out)
	case r.URL.Path == "/api/sessions/"+pane+"/stream":
		s.stream(w, withReadOnly(r))
	case r.URL.Path == "/api/sessions/"+pane+"/output":
		s.readOutput(w, r)
	case r.URL.Path == "/api/config":
		writeJSON(w, http.StatusOK, map[string]any{"version": s.opts.Version, "readOnly": true})
	case r.URL.Path == "/api/profiles":
		writeJSON(w, http.StatusOK, []Profile{})
	case r.URL.Path == "/api/layout":
		writeJSON(w, http.StatusOK, nil)
	default:
		writeError(w, http.StatusForbidden, "read_only", "This link opens one pane only.")
	}
}

type readOnlyKey struct{}

func withReadOnly(r *http.Request) *http.Request {
	return r.WithContext(contextWith(r.Context(), readOnlyKey{}, true))
}

func isReadOnly(r *http.Request) bool {
	v, _ := r.Context().Value(readOnlyKey{}).(bool)
	return v
}

// createShare is POST /api/shares {pane, for}: a read-only link to a pane.
func (s *Server) createShare(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Pane string `json:"pane"`
		For  string `json:"for"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "input", "The body is not JSON.")
		return
	}
	if _, ok := s.sessions.get(body.Pane); !ok {
		writeError(w, http.StatusNotFound, "no_session", errNoSession.Error())
		return
	}
	life := 24 * time.Hour
	if body.For != "" {
		d, err := time.ParseDuration(body.For)
		if err != nil || d <= 0 {
			writeError(w, http.StatusBadRequest, "input", "for is a duration such as 2h.")
			return
		}
		life = min(d, maxShareFor)
	}
	now := time.Now().UTC()
	sh := Share{Token: NewToken(), Pane: body.Pane, Created: now, Expires: now.Add(life)}
	s.shares.mu.Lock()
	s.shares.all[sh.Token] = sh
	s.shares.saveLocked()
	s.shares.mu.Unlock()
	writeJSON(w, http.StatusCreated, sh)
}

func (s *Server) listShares(w http.ResponseWriter, r *http.Request) {
	s.shares.mu.Lock()
	defer s.shares.mu.Unlock()
	out := []Share{}
	now := time.Now()
	for _, sh := range s.shares.all {
		if now.Before(sh.Expires) {
			out = append(out, sh)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) dropShare(w http.ResponseWriter, r *http.Request) {
	s.shares.mu.Lock()
	defer s.shares.mu.Unlock()
	if _, ok := s.shares.all[r.PathValue("token")]; !ok {
		writeError(w, http.StatusNotFound, "no_share", "There is no such share.")
		return
	}
	delete(s.shares.all, r.PathValue("token"))
	s.shares.saveLocked()
	w.WriteHeader(http.StatusNoContent)
}

func contextWith(ctx context.Context, key, value any) context.Context {
	return context.WithValue(ctx, key, value)
}
