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
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RunOutcome/laforge/holder"
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
	// Holders runs each pane in a holder process of its own, so panes outlive
	// the forge; nil runs them in this process. At start the forge adopts
	// the panes a forge before it left running.
	Holders *holder.Launcher
}

// Server is the forge's HTTP surface.
type Server struct {
	opts     Options
	sessions *sessions
	mux      *http.ServeMux
	mu       sync.Mutex
	layoutMu sync.Mutex
	cwd      string
	started  time.Time
	events   *hub
	last     map[string]Pane // each pane's last view, for its pane.closed event
	restart  chan struct{}
	shares   *shares
	// collCache keeps what skin collections offer for a while.
	collCache collectionCache
	stop      chan struct{}
	stopOnce  sync.Once
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
	s := &Server{opts: opts, mux: http.NewServeMux(), cwd: opts.Cwd, started: time.Now(), events: newHub(), last: map[string]Pane{}, restart: make(chan struct{}, 1)}
	s.shares = newShares(opts.Home)
	s.sessions = newSessions(s.changed)
	s.sessions.home = opts.Home
	s.sessions.holders = opts.Holders
	s.sessions.adopt(opts.Scrollback)
	for _, p := range s.sessions.list() {
		s.last[p.ID] = p
	}
	s.stop = make(chan struct{})
	go s.settle(5*time.Second, s.stop)
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
func (s *Server) Shutdown(grace time.Duration) {
	s.stopOnce.Do(func() { close(s.stop) })
	s.sessions.stopAll(grace)
}

// Detach lets go of every held pane and leaves it running for the next
// forge to adopt (a restart, an update). Panes this process runs itself
// (no holders) end with it.
func (s *Server) Detach() {
	s.stopOnce.Do(func() { close(s.stop) })
	s.sessions.detachAll()
}

// Restarts delivers a request (POST /api/restart) for the forge to be
// restarted with its panes kept; the host does the restart.
func (s *Server) Restarts() <-chan struct{} { return s.restart }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) routes(web fs.FS) {
	s.mux.HandleFunc("GET /api/health", s.health)
	s.mux.HandleFunc("GET /api/profiles", s.auth(s.listProfiles))
	s.mux.HandleFunc("GET /api/sessions", s.auth(s.listSessions))
	s.mux.HandleFunc("POST /api/sessions", s.auth(s.startSession))
	s.mux.HandleFunc("PATCH /api/sessions/{id}", s.auth(s.renameSession))
	s.mux.HandleFunc("DELETE /api/sessions/{id}", s.auth(s.closeSession))
	s.mux.HandleFunc("GET /api/sessions/{id}/stream", s.auth(s.stream))
	s.mux.HandleFunc("POST /api/sessions/{id}/status", s.auth(s.reportStatus))
	s.mux.HandleFunc("POST /api/sessions/{id}/seen", s.auth(s.markSeen))
	s.mux.HandleFunc("GET /api/events", s.auth(s.streamEvents))
	s.mux.HandleFunc("POST /api/sessions/{id}/agent", s.auth(s.setAgentSession))
	s.mux.HandleFunc("GET /api/lost", s.auth(s.listLost))
	s.mux.HandleFunc("DELETE /api/lost/{id}", s.auth(s.dropLost))
	s.mux.HandleFunc("POST /api/restart", s.auth(s.requestRestart))
	s.mux.HandleFunc("POST /api/sessions/{id}/input", s.auth(s.sendInput))
	s.mux.HandleFunc("GET /api/sessions/{id}/output", s.auth(s.readOutput))
	s.mux.HandleFunc("GET /api/whoami", s.auth(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"mode": "full"})
	}))
	s.mux.HandleFunc("POST /api/sessions/{id}/ask", s.auth(s.askPermission))
	s.mux.HandleFunc("POST /api/sessions/{id}/answer", s.auth(s.answerAsk))
	s.mux.HandleFunc("GET /api/skins", s.auth(s.listSkins))
	s.mux.HandleFunc("GET /api/skins/collections", s.auth(s.listCollections))
	s.mux.HandleFunc("POST /api/skins/collections", s.auth(s.addCollection))
	s.mux.HandleFunc("DELETE /api/skins/collections/{id}", s.auth(s.removeCollection))
	s.mux.HandleFunc("GET /api/skins/collections/{id}/skins", s.auth(s.collectionSkins))
	s.mux.HandleFunc("PUT /api/skins/default", s.auth(s.setDefaultSkin))
	s.mux.HandleFunc("DELETE /api/skins/{name}", s.auth(s.removeSkin))
	s.mux.HandleFunc("POST /api/skins/install", s.auth(s.installSkins))
	s.mux.HandleFunc("POST /api/skins/{name}/update", s.auth(s.updateSkin))
	s.mux.HandleFunc("POST /api/skins/{name}/code", s.auth(s.skinCode))
	s.mux.HandleFunc("PUT /api/skins/scenes", s.auth(s.setScenes))
	// a scene's frame cannot present the cookie; its key is its pass
	s.mux.HandleFunc("GET /scene/{key}/{name}/{file...}", s.sceneFile)
	s.mux.HandleFunc("GET /skins/{name}/{file...}", func(w http.ResponseWriter, r *http.Request) {
		if _, viewing := s.viewer(r); !s.authorized(r) && !viewing {
			http.Error(w, "Present this forge's token.", http.StatusUnauthorized)
			return
		}
		s.skinFile(w, r)
	})
	s.mux.HandleFunc("POST /api/shares", s.auth(s.createShare))
	s.mux.HandleFunc("GET /api/shares", s.auth(s.listShares))
	s.mux.HandleFunc("DELETE /api/shares/{token}", s.auth(s.dropShare))
	s.mux.HandleFunc("GET /api/layout", s.auth(s.getLayout))
	s.mux.HandleFunc("PUT /api/layout", s.auth(s.putLayout))
	s.mux.HandleFunc("GET /api/config", s.auth(s.getConfig))
	s.mux.HandleFunc("PUT /api/config", s.auth(s.putConfig))
	files := http.FileServerFS(web)
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if v := r.URL.Query().Get("view"); v != "" {
			if _, ok := s.shares.pane(v); !ok {
				http.Error(w, "This share link has expired or was revoked.", http.StatusUnauthorized)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: viewCookie, Value: v, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
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
			if _, viewing := s.viewer(r); !s.authorized(r) && !viewing {
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
			if pane, ok := s.viewer(r); ok {
				s.viewOnly(w, r, pane)
				return
			}
			writeError(w, http.StatusUnauthorized, "unauthorized", "Present this forge's token.")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !sameOrigin(r) {
			writeError(w, http.StatusForbidden, "cross_origin", "Changes come only from the forge's own page.")
			return
		}
		next(w, r)
	}
}

// sameOrigin refuses a browser request sent from another origin. The cookie
// is SameSite=Strict, but every port on 127.0.0.1 is the same site, so a page
// on another local port could otherwise send it with a simple POST. Browsers
// send Origin (and Sec-Fetch-Site) on these requests; CLI clients send
// neither and authenticate with the bearer token.
func sameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host == r.Host
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
	if req.Parent != "" {
		if _, ok := s.sessions.own(req.Parent); !ok { // the parent may be starting
			writeError(w, http.StatusBadRequest, "no_parent", "There is no pane "+req.Parent+" to be the parent.")
			return
		}
	}
	if len(req.Argv) > 0 {
		s.startCommand(w, req)
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
	if req.Resume != "" {
		lost, ok := s.sessions.takeLost(req.Resume)
		if !ok {
			writeError(w, http.StatusNotFound, "no_session", "There is no such lost pane.")
			return
		}
		if req.Cwd == "" {
			req.Cwd = lost.Cwd
		}
		if req.Title == "" && lost.Title != chosen.Label {
			req.Title = lost.Title
		}
		if lost.AgentSession != nil && len(chosen.Resume) > 0 && lost.Profile == chosen.Name {
			c := *chosen
			c.Args = resumeArgs(c, lost.AgentSession.SessionID)
			chosen = &c
		}
	}
	if len(req.Args) > 0 {
		c := *chosen
		c.Args = append(append([]string{}, c.Args...), req.Args...)
		chosen = &c
	}
	pane, err := s.sessions.start(*chosen, req, s.defaultCwd(), s.opts.Scrollback)
	if err != nil {
		writeError(w, http.StatusBadRequest, "start", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, pane)
}

// startCommand starts req.Argv in a pane of its own (no profile).
func (s *Server) startCommand(w http.ResponseWriter, req StartRequest) {
	if strings.TrimSpace(req.Argv[0]) == "" {
		writeError(w, http.StatusBadRequest, "input", "argv names no command.")
		return
	}
	p := Profile{Name: "command", Label: strings.Join(req.Argv, " "), Command: req.Argv[0], Args: req.Argv[1:]}
	if len(p.Label) > 60 {
		p.Label = p.Label[:59] + "…"
	}
	pane, err := s.sessions.start(p, req, s.defaultCwd(), s.opts.Scrollback)
	if err != nil {
		writeError(w, http.StatusBadRequest, "start", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, pane)
}

func (s *Server) requestRestart(w http.ResponseWriter, r *http.Request) {
	select {
	case s.restart <- struct{}{}:
	default:
	}
	w.WriteHeader(http.StatusAccepted)
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

// closeFellBehind closes a stream whose viewer missed output; the client
// reconnects and redraws from the buffer.
const closeFellBehind websocket.StatusCode = 4408

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
			if isReadOnly(r) {
				continue // a shared view only watches
			}
			switch m.Type {
			case "input":
				e.markHuman()
				_ = e.session.Write([]byte(m.Data))
			case "resize":
				_ = e.session.Resize(m.Cols, m.Rows)
			}
		}
	}()

	var offset int64
	sent := false
	for {
		watch := e.session.WatchFrom(offset)
		if watch.Dropped > 0 && sent {
			// The ring wrapped past what this viewer had: rather than draw a
			// screen with a hole in it, close so the client redraws whole.
			watch.Cancel()
			conn.Close(closeFellBehind, "fell behind")
			return
		}
		sent = true
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
	s.layoutMu.Lock()
	defer s.layoutMu.Unlock()
	tmp, err := os.CreateTemp(s.opts.Home, "layout-*.json")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "layout", err.Error())
		return
	}
	_, werr := tmp.Write(raw)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmp.Name())
		writeError(w, http.StatusInternalServerError, "layout", errors.Join(werr, cerr).Error())
		return
	}
	if err := os.Rename(tmp.Name(), s.layoutPath()); err != nil {
		os.Remove(tmp.Name())
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
