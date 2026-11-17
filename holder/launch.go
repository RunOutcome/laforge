// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package holder

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Launcher starts holders.
type Launcher struct {
	// Dir holds the sockets and logs (0700).
	Dir string
	// Command runs a holder: this program and the arguments that reach
	// RunFromStdin ("la holder", "out la holder").
	Command []string
	// InProcess runs each holder as goroutines of this process instead:
	// the panes then end with it. For tests.
	InProcess bool
}

// Supported reports whether panes can be held on this platform.
func Supported() bool { return supported }

// Usable reports whether pane id's socket path fits.
func (l *Launcher) Usable(id string) bool {
	return l != nil && supported && len(Socket(l.Dir, id)) <= MaxSocketPath
}

type startRequest struct {
	Spec Spec            `json:"spec"`
	Meta json.RawMessage `json:"meta,omitempty"`
}

// Start starts a holder for spec and returns once its socket answers.
func (l *Launcher) Start(spec Spec, meta json.RawMessage) error {
	if err := os.MkdirAll(l.Dir, 0o700); err != nil {
		return err
	}
	if Alive(l.Dir, spec.ID) {
		return fmt.Errorf("holder: %s is already held", spec.ID)
	}
	if l.InProcess {
		errc := make(chan error, 1)
		go func() { errc <- Serve(l.Dir, spec, meta) }()
		return l.waitReady(spec.ID, errc, "")
	}
	if len(l.Command) == 0 {
		return errors.New("holder: no command to start holders with")
	}
	raw, _ := json.Marshal(startRequest{Spec: spec, Meta: meta})
	logPath := filepath.Join(l.Dir, spec.ID+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	cmd := exec.Command(l.Command[0], l.Command[1:]...)
	cmd.Stdin = bytes.NewReader(raw)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.Dir = spec.Workdir
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	errc := make(chan error, 1)
	go func() {
		err := cmd.Wait() // reaps it while this forge lives
		if err == nil {
			err = errors.New("the holder exited")
		}
		errc <- err
	}()
	return l.waitReady(spec.ID, errc, logPath)
}

func (l *Launcher) waitReady(id string, errc <-chan error, logPath string) error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.Dial("unix", Socket(l.Dir, id)); err == nil {
			c.Close()
			return nil
		}
		select {
		case err := <-errc:
			return fmt.Errorf("holder for %s: %v%s", id, err, logTail(logPath))
		case <-time.After(20 * time.Millisecond):
		}
	}
	return fmt.Errorf("holder for %s did not start%s", id, logTail(logPath))
}

func logTail(path string) string {
	if path == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(bytes.TrimSpace(raw)) == 0 {
		return ""
	}
	text := strings.TrimSpace(string(raw))
	if len(text) > 400 {
		text = text[len(text)-400:]
	}
	return ": " + text
}

// RunFromStdin is the holder process's main: it reads its start request
// from stdin and serves until it is done.
func RunFromStdin(dir string, stdin io.Reader) error {
	var req startRequest
	if err := json.NewDecoder(io.LimitReader(stdin, 1<<20)).Decode(&req); err != nil {
		return fmt.Errorf("holder: bad start request: %w", err)
	}
	ignoreHangup()
	return Serve(dir, req.Spec, req.Meta)
}

// Alive reports whether pane id has a holder answering in dir.
func Alive(dir, id string) bool {
	c, err := net.DialTimeout("unix", Socket(dir, id), time.Second)
	if err != nil {
		return false
	}
	c.Close()
	return true
}
