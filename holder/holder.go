// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package holder

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RunOutcome/laforge/terminal"
)

// Spec is what a holder runs: a terminal.LaunchSpec that can travel as JSON.
// The holder inherits the environment of the forge that starts it, so
// InheritAll and Unset mean what they mean in the forge.
type Spec struct {
	ID         string   `json:"id"`
	Command    string   `json:"command"`
	Args       []string `json:"args,omitempty"`
	Workdir    string   `json:"workdir"`
	Env        []string `json:"env,omitempty"`
	InheritAll bool     `json:"inheritAll,omitempty"`
	Unset      []string `json:"unset,omitempty"`
	Columns    uint16   `json:"cols,omitempty"`
	Rows       uint16   `json:"rows,omitempty"`
	Scrollback int      `json:"scrollback,omitempty"`
	// ExitPipe gives the program descriptor 3, a pipe on which the agent
	// wrapper reports the agent's exit code; the holder passes it on.
	ExitPipe bool `json:"exitPipe,omitempty"`
}

func (s Spec) launch() terminal.LaunchSpec {
	return terminal.LaunchSpec{Command: s.Command, Args: s.Args, Workdir: s.Workdir, Env: s.Env, Columns: s.Columns, Rows: s.Rows, InheritAll: s.InheritAll, Unset: s.Unset}
}

// View is what a holder tells its forge about the pane.
type View struct {
	Session terminal.SessionView `json:"session"`
	// Meta is the forge's record of the pane, as the forge last gave it.
	Meta json.RawMessage `json:"meta,omitempty"`
	// AgentExit is the agent's exit code, once the wrapper reported it.
	AgentExit *int `json:"agentExit,omitempty"`
	HolderPID int  `json:"holderPid"`
	// Proto is the holder's protocol version. Holders outlive upgrades, so
	// a newer forge reads it before relying on anything a later version added.
	Proto int `json:"proto"`
}

// Proto is this build's holder protocol version.
const Proto = 1

// Socket is the path of pane id's holder socket in dir.
func Socket(dir, id string) string { return filepath.Join(dir, id+".sock") }

// MaxSocketPath is the longest socket path every Unix accepts (macOS: 104).
const MaxSocketPath = 100

// linger is how long a holder whose program has ended waits for a forge
// to collect the end before it goes.
var linger = 30 * time.Minute

type holder struct {
	dir     string
	spec    Spec
	session *terminal.Session
	ln      net.Listener

	mu        sync.Mutex
	meta      json.RawMessage
	agentExit *int
	conns     map[*conn]bool
	forgotten bool
	lastConn  time.Time
	wake      chan struct{}
}

type conn struct {
	c  net.Conn
	mu sync.Mutex // one writer at a time
}

func (c *conn) send(typ byte, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.c.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return writeFrame(c.c, typ, payload)
}

// Serve runs spec in a pseudo-terminal held by this process and serves it on
// its socket in dir until the forge forgets it (or the program has ended and
// no forge has come for linger). meta is the forge's record of the pane.
func Serve(dir string, spec Spec, meta json.RawMessage) error {
	if spec.ID == "" || strings.ContainsAny(spec.ID, "/\\") {
		return errors.New("holder: a pane id is required")
	}
	path := Socket(dir, spec.ID)
	if len(path) > MaxSocketPath {
		return fmt.Errorf("holder: socket path too long (%d): %s", len(path), path)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if c, err := net.Dial("unix", path); err == nil {
		c.Close()
		return fmt.Errorf("holder: %s is already held", spec.ID)
	}
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	defer os.Remove(path)
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return err
	}
	h := &holder{dir: dir, spec: spec, ln: ln, meta: meta, conns: map[*conn]bool{}, lastConn: time.Now(), wake: make(chan struct{}, 1)}

	launch := spec.launch()
	var exits *os.File
	if spec.ExitPipe {
		r, w, err := os.Pipe()
		if err != nil {
			ln.Close()
			return err
		}
		exits, launch.ExtraFiles = r, []*os.File{w}
	}
	session, err := terminal.Start(spec.ID, launch, spec.Scrollback, func(*terminal.Session) { h.broadcast() })
	for _, f := range launch.ExtraFiles {
		f.Close()
	}
	if err != nil {
		ln.Close()
		if exits != nil {
			exits.Close()
		}
		return err
	}
	h.mu.Lock()
	h.session = session
	h.mu.Unlock()
	if exits != nil {
		go h.watchAgentExit(exits)
	}
	go h.accept()
	h.run()
	ln.Close()
	h.mu.Lock()
	for c := range h.conns {
		c.c.Close()
	}
	h.mu.Unlock()
	return nil
}

// run waits until the holder may go: forgotten and ended, or ended with no
// forge for linger.
func (h *holder) run() {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	done := h.session.Done()
	for {
		h.mu.Lock()
		ended := isEnded(h.session)
		forgotten := h.forgotten
		alone := len(h.conns) == 0 && time.Since(h.lastConn) > linger
		h.mu.Unlock()
		if ended && (forgotten || alone) {
			return
		}
		select {
		case <-h.wake:
		case <-done:
			done = nil // ended: from now on only a forget or the linger ends the wait
		case <-tick.C:
		}
	}
}

func isEnded(s *terminal.Session) bool {
	select {
	case <-s.Done():
		return true
	default:
		return false
	}
}

func (h *holder) poke() {
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

func (h *holder) watchAgentExit(r *os.File) {
	defer r.Close()
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil {
		return
	}
	code, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		return
	}
	h.mu.Lock()
	h.agentExit = &code
	h.mu.Unlock()
	h.broadcast()
}

func (h *holder) view() View {
	h.mu.Lock()
	defer h.mu.Unlock()
	return View{Session: h.session.View(), Meta: h.meta, AgentExit: h.agentExit, HolderPID: os.Getpid(), Proto: Proto}
}

func (h *holder) broadcast() {
	h.mu.Lock()
	if h.session == nil {
		h.mu.Unlock()
		return
	}
	h.mu.Unlock()
	raw, _ := json.Marshal(h.view())
	h.mu.Lock()
	conns := make([]*conn, 0, len(h.conns))
	for c := range h.conns {
		conns = append(conns, c)
	}
	h.mu.Unlock()
	for _, c := range conns {
		if c.send(frameView, raw) != nil {
			c.c.Close()
		}
	}
}

func (h *holder) accept() {
	for {
		nc, err := h.ln.Accept()
		if err != nil {
			return
		}
		go h.serveConn(&conn{c: nc})
	}
}

func (h *holder) serveConn(c *conn) {
	h.mu.Lock()
	h.conns[c] = true
	h.lastConn = time.Now()
	h.mu.Unlock()
	defer func() {
		c.c.Close()
		h.mu.Lock()
		delete(h.conns, c)
		h.lastConn = time.Now()
		h.mu.Unlock()
		h.poke()
	}()
	raw, _ := json.Marshal(h.view())
	if c.send(frameView, raw) != nil {
		return
	}
	stop := make(chan struct{})
	defer close(stop)
	for {
		typ, payload, err := readFrame(c.c)
		if err != nil {
			return
		}
		switch typ {
		case frameAttach:
			var a struct {
				From int64 `json:"from"`
			}
			_ = json.Unmarshal(payload, &a)
			go h.stream(c, a.From, stop)
		case frameInput:
			_ = h.session.Write(payload)
		case frameResize:
			var r struct{ Cols, Rows uint16 }
			if json.Unmarshal(payload, &r) == nil {
				_ = h.session.Resize(r.Cols, r.Rows)
			}
		case frameStop:
			var s struct{ Force bool }
			_ = json.Unmarshal(payload, &s)
			_ = h.session.Stop(s.Force)
		case frameReport:
			var r terminal.Report
			if json.Unmarshal(payload, &r) == nil {
				_, _ = h.session.Report(r)
			}
		case frameMeta:
			h.mu.Lock()
			h.meta = append(json.RawMessage(nil), payload...)
			h.mu.Unlock()
		case frameForget:
			h.mu.Lock()
			h.forgotten = true
			h.mu.Unlock()
			if !isEnded(h.session) {
				_ = h.session.Stop(true)
			}
			h.poke()
		}
	}
}

// stream sends the output from offset on, then the status records as they
// are now (reports that have left the ring), then live output, until the
// program ends (frameEnd) or the connection goes.
func (h *holder) stream(c *conn, offset int64, stop <-chan struct{}) {
	first := true
	for {
		w := h.session.WatchFrom(offset)
		if len(w.Output) > 0 && c.send(frameOutput, w.Output) != nil {
			w.Cancel()
			return
		}
		offset = w.From + int64(len(w.Output))
		if first {
			first = false
			if replay := h.session.StatusReplay(); len(replay) > 0 && c.send(frameOutput, replay) != nil {
				w.Cancel()
				return
			}
		}
		if w.Ended {
			// the exit code first, so the end the forge reads carries it
			raw, _ := json.Marshal(h.view())
			_ = c.send(frameView, raw)
			_ = c.send(frameEnd, nil)
			return
		}
	feed:
		for {
			select {
			case <-stop:
				w.Cancel()
				return
			case chunk, open := <-w.Feed:
				if !open {
					break feed
				}
				if c.send(frameOutput, chunk) != nil {
					w.Cancel()
					return
				}
				offset += int64(len(chunk))
			}
		}
	}
}
