// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// Package forge is the LaForge backend: it runs shells and agent CLIs in
// pseudo-terminals, keeps them alive while browsers come and go, and serves
// the Forge web client that lays them out as tabs and split panes.
//
// Server is an http.Handler so a host program (the la binary, or Outcome)
// can serve it on a listener of its own.
package forge

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Options configure a Server.
type Options struct {
	// Home is the forge's folder (layout.json, profiles.json).
	Home string
	// Token is the secret a browser presents once (?token=) to get its cookie,
	// and a client presents as a bearer token. Empty generates one.
	Token string
	// Cwd is the folder new panes start in when the client names none.
	Cwd string
	// Version is reported by /api/health.
	Version string
	// Scrollback is each session's replay buffer in bytes (0: 256 KiB).
	Scrollback int
	// Web replaces the embedded client (tests, development).
	Web fs.FS
}

// Server is the forge's HTTP surface.
type Server struct {
	opts     Options
	sessions *sessions
	mux      *http.ServeMux
	mu       sync.Mutex
	cwd      string
	started  time.Time
}

const cookieName = "laforge_token"

// NewServer builds a forge server.
func NewServer(opts Options) (*Server, error) {
	if opts.Home == "" {
		return nil, errors.New("forge: Home is required")
	}
	if err := os.MkdirAll(opts.Home, 0o700); err != nil {
		return nil, err
	}
	if opts.Token == "" {
		opts.Token = NewToken()
	}
	if opts.Cwd == "" {
		opts.Cwd, _ = os.UserHomeDir()
	}
	web := opts.Web
	if web == nil {
		sub, err := fs.Sub(embedded, "dist")
		if err != nil {
			return nil, err
		}
		web = sub
	}
	s := &Server{opts: opts, sessions: newSessions(nil), mux: http.NewServeMux(), cwd: opts.Cwd, started: time.Now()}
	s.routes(web)
	return s, nil
}

// NewToken is a fresh random secret.
func NewToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Token is the server's secret.
func (s *Server) Token() string { return s.opts.Token }

// SetCwd changes the folder new panes start in.
func (s *Server) SetCwd(dir string) {
	s.mu.Lock()
	s.cwd = dir
	s.mu.Unlock()
}

func (s *Server) defaultCwd() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cwd
}

// Shutdown stops every session, waiting up to grace for them to exit.
func (s *Server) Shutdown(grace time.Duration) { s.sessions.stopAll(grace) }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) routes(web fs.FS) {
	s.mux.HandleFunc("GET /api/health", s.health)
	s.mux.HandleFunc("GET /api/profiles", s.auth(s.listProfiles))
	s.mux.HandleFunc("GET /api/sessions", s.auth(s.listSessions))
	s.mux.HandleFunc("POST /api/sessions", s.auth(s.startSession))
	s.mux.HandleFunc("PATCH /api/sessions/{id}", s.auth(s.renameSession))
	s.mux.HandleFunc("DELETE /api/sessions/{id}", s.auth(s.closeSession))
	s.mux.HandleFunc("GET /api/sessions/{id}/stream", s.auth(s.stream))
	s.mux.HandleFunc("GET /api/layout", s.auth(s.getLayout))
	s.mux.HandleFunc("PUT /api/layout", s.auth(s.putLayout))
	s.mux.HandleFunc("GET /api/config", s.auth(s.getConfig))
	s.mux.HandleFunc("PUT /api/config", s.auth(s.putConfig))
	files := http.FileServerFS(web)
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if t := r.URL.Query().Get("token"); t != "" {
			if !s.tokenOK(t) {
				http.Error(w, "This link's token is not this forge's. Run la open for a fresh link.", http.StatusUnauthorized)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: cookieName, Value: t, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			w.Header().Set("Cache-Control", "no-store")
			if !s.authorized(r) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusUnauthorized)
				io.WriteString(w, lockedPage)
				return
			}
		}
		files.ServeHTTP(w, r)
	})
}

const lockedPage = `<!doctype html><meta charset="utf-8"><title>LaForge</title>
<style>body{font:15px/1.5 system-ui;background:#111418;color:#e8ebf1;display:grid;place-items:center;height:100vh;margin:0}
code{background:#20242b;padding:2px 6px;border-radius:4px}</style>
<div><h2>LaForge is locked</h2><p>Open it with the link from <code>la open</code> (or <code>out la open</code>).</p></div>`

func (s *Server) tokenOK(t string) bool {
	return subtle.ConstantTimeCompare([]byte(t), []byte(s.opts.Token)) == 1
}

func (s *Server) authorized(r *http.Request) bool {
	if c, err := r.Cookie(cookieName); err == nil && s.tokenOK(c.Value) {
		return true
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") && s.tokenOK(strings.TrimPrefix(h, "Bearer ")) {
		return true
	}
	return false
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Present this forge's token.")
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, reason string) {
	writeJSON(w, status, map[string]string{"error": code, "reason": reason})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "product": "laforge", "version": s.opts.Version, "pid": os.Getpid(), "startedAt": s.started})
}

func (s *Server) profiles() ([]Profile, error) {
	p, err := loadProfiles(s.opts.Home)
	if err != nil {
		return nil, err
	}
	return withAvailability(p), nil
}

func (s *Server) listProfiles(w http.ResponseWriter, r *http.Request) {
	p, err := s.profiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "profiles", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.sessions.list())
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request) {
	var req StartRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "input", "The body is not JSON.")
		return
	}
	if req.Profile == "" {
		req.Profile = "shell"
	}
	profiles, err := s.profiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "profiles", err.Error())
		return
	}
	var chosen *Profile
	for i := range profiles {
		if profiles[i].Name == req.Profile {
			chosen = &profiles[i]
		}
	}
	if chosen == nil {
		writeError(w, http.StatusBadRequest, "no_profile", "There is no profile "+req.Profile+".")
		return
	}
	if !chosen.Available {
		writeError(w, http.StatusBadRequest, "profile_unavailable", chosen.Reason)
		return
	}
	pane, err := s.sessions.start(*chosen, req, s.defaultCwd(), s.opts.Scrollback)
	if err != nil {
		writeError(w, http.StatusBadRequest, "start", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, pane)
}

func (s *Server) renameSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "input", "The body is not JSON.")
		return
	}
	pane, err := s.sessions.rename(r.PathValue("id"), body.Title)
	if err != nil {
		writeError(w, http.StatusNotFound, "no_session", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, pane)
}

func (s *Server) closeSession(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("force") == "1"
	if err := s.sessions.close(r.PathValue("id"), force, 3*time.Second); err != nil {
		writeError(w, http.StatusNotFound, "no_session", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// clientMessage is what the browser sends on a stream as a text frame.
type clientMessage struct {
	Type string `json:"type"` // input | resize
	Data string `json:"data,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

// stream is the terminal's WebSocket: the retained output first, then live
// output, as binary frames; input and resizes come back as JSON text frames.
// It closes normally with reason "ended" when the session has ended.
func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	e, ok := s.sessions.get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "no_session", errNoSession.Error())
		return
	}
	conn, err := websocket.Accept(w, r, nil) // same-origin only
	if err != nil {
		return
	}
	conn.SetReadLimit(1 << 20)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go func() {
		defer cancel()
		for {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if typ != websocket.MessageText {
				continue
			}
			var m clientMessage
			if json.Unmarshal(data, &m) != nil {
				continue
			}
			switch m.Type {
			case "input":
				_ = e.session.Write([]byte(m.Data))
			case "resize":
				_ = e.session.Resize(m.Cols, m.Rows)
			}
		}
	}()

	var offset int64
	for {
		watch := e.session.WatchFrom(offset)
		if len(watch.Output) > 0 {
			if conn.Write(ctx, websocket.MessageBinary, watch.Output) != nil {
				watch.Cancel()
				return
			}
		}
		offset = watch.From + int64(len(watch.Output))
		if watch.Ended {
			conn.Close(websocket.StatusNormalClosure, "ended")
			return
		}
	feed:
		for {
			select {
			case <-ctx.Done():
				watch.Cancel()
				conn.CloseNow()
				return
			case chunk, open := <-watch.Feed:
				if !open {
					break feed // ended or let go: rejoin where we stopped
				}
				if conn.Write(ctx, websocket.MessageBinary, chunk) != nil {
					watch.Cancel()
					return
				}
				offset += int64(len(chunk))
			}
		}
	}
}

func (s *Server) layoutPath() string { return filepath.Join(s.opts.Home, "layout.json") }

func (s *Server) getLayout(w http.ResponseWriter, r *http.Request) {
	raw, err := os.ReadFile(s.layoutPath())
	if errors.Is(err, os.ErrNotExist) {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "layout", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(raw)
}

func (s *Server) putLayout(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil || !json.Valid(raw) {
		writeError(w, http.StatusBadRequest, "input", "The layout is not JSON.")
		return
	}
	tmp := s.layoutPath() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		writeError(w, http.StatusInternalServerError, "layout", err.Error())
		return
	}
	if err := os.Rename(tmp, s.layoutPath()); err != nil {
		writeError(w, http.StatusInternalServerError, "layout", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) {
	host, _ := os.Hostname()
	user := os.Getenv("USER")
	writeJSON(w, http.StatusOK, map[string]any{"cwd": s.defaultCwd(), "host": host, "user": user, "version": s.opts.Version})
}

func (s *Server) putConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Cwd string `json:"cwd"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "input", "The body is not JSON.")
		return
	}
	if dir := expandHome(body.Cwd); dir != "" {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			writeError(w, http.StatusBadRequest, "input", dir+" is not a folder.")
			return
		}
		s.SetCwd(dir)
	}
	s.getConfig(w, r)
}

// Sessions lists the forge's sessions (for status reports).
func (s *Server) Sessions() []Pane { return s.sessions.list() }
