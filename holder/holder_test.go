// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

//go:build !windows

package holder

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RunOutcome/laforge/terminal"
)

func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "lfh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// reader collects a client's output until a string appears.
type reader struct {
	t   *testing.T
	buf bytes.Buffer
	ch  chan []byte
}

func readAll(t *testing.T, r io.Reader) *reader {
	rd := &reader{t: t, ch: make(chan []byte, 256)}
	go func() {
		b := make([]byte, 4096)
		for {
			n, err := r.Read(b)
			if n > 0 {
				rd.ch <- append([]byte(nil), b[:n]...)
			}
			if err != nil {
				close(rd.ch)
				return
			}
		}
	}()
	return rd
}

func (rd *reader) until(s string) string {
	rd.t.Helper()
	deadline := time.After(5 * time.Second)
	for !strings.Contains(rd.buf.String(), s) {
		select {
		case b, ok := <-rd.ch:
			if !ok {
				rd.t.Fatalf("ended waiting for %q: %q", s, rd.buf.String())
			}
			rd.buf.Write(b)
		case <-deadline:
			rd.t.Fatalf("no %q in %q", s, rd.buf.String())
		}
	}
	return rd.buf.String()
}

// A held program keeps running when its forge goes: a new client gets the
// retained output and the status records as they are, takes input and
// reads the exit; Forget ends the holder.
func TestAHeldProgramOutlivesItsClient(t *testing.T) {
	dir := shortDir(t)
	l := &Launcher{Dir: dir, InProcess: true}
	spec := Spec{ID: "s-held", Command: "/bin/sh", Args: []string{"-c", `printf 'one\n'; printf '\033]7501;state=working:app=fake\033\\'; read a; echo "got $a"; read b; exit 7`}, Workdir: dir}
	if err := l.Start(spec, json.RawMessage(`{"title":"first"}`)); err != nil {
		t.Fatal(err)
	}
	c1, err := Dial(dir, "s-held", nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(c1.First().Meta) != `{"title":"first"}` || c1.First().HolderPID != os.Getpid() || c1.PID() == 0 {
		t.Fatalf("first view %+v", c1.First())
	}
	r1 := readAll(t, c1)
	r1.until("one")
	c1.Report(terminal.Report{Status: terminal.Status{State: "blocked", Kind: "question", Msg: "Which?", Source: "hook"}})
	c1.SetMeta(json.RawMessage(`{"title":"renamed"}`))
	time.Sleep(100 * time.Millisecond)
	c1.Detach() // the forge goes away

	c2, err := Dial(dir, "s-held", nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(c2.First().Meta) != `{"title":"renamed"}` {
		t.Fatalf("meta %s", c2.First().Meta)
	}
	if st := c2.First().Session.Status; st == nil || st.State != "blocked" || st.Source != "hook" {
		t.Fatalf("status %+v", st)
	}
	r2 := readAll(t, c2)
	out := r2.until("state=blocked")
	if !strings.Contains(out, "one") || strings.Index(out, "state=working") > strings.Index(out, "state=blocked") {
		t.Fatalf("replay %q", out)
	}
	c2.Write([]byte("hello\n"))
	r2.until("got hello")
	c2.Write([]byte("bye\n"))
	code, err := c2.Wait()
	if code != 7 || err != nil {
		t.Fatalf("wait %d %v", code, err)
	}
	if !Alive(dir, "s-held") {
		t.Fatal("the holder went before it was forgotten")
	}
	c2.Forget()
	deadline := time.Now().Add(5 * time.Second)
	for Alive(dir, "s-held") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if Alive(dir, "s-held") {
		t.Fatal("the holder stayed after Forget")
	}
	if _, err := os.Stat(Socket(dir, "s-held")); !os.IsNotExist(err) {
		t.Fatalf("socket left: %v", err)
	}
}

// The agent wrapper's exit report reaches the client; a pane id is held
// once; Forget ends a program that still runs.
func TestTheAgentExitIsPassedOnAndForgetEndsAProgram(t *testing.T) {
	dir := shortDir(t)
	l := &Launcher{Dir: dir, InProcess: true}
	spec := Spec{ID: "s-agent", Command: "/bin/sh", Args: []string{"-c", `echo 3 >&3; exec 3>&-; sleep 60`}, Workdir: dir, ExitPipe: true}
	if err := l.Start(spec, nil); err != nil {
		t.Fatal(err)
	}
	if err := l.Start(spec, nil); err == nil {
		t.Fatal("a second holder for the same pane started")
	}
	c, err := Dial(dir, "s-agent", nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-c.AgentExit():
		if code != 3 {
			t.Fatalf("agent exit %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no agent exit")
	}
	c.Forget()
	deadline := time.Now().Add(5 * time.Second)
	for Alive(dir, "s-agent") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if Alive(dir, "s-agent") {
		t.Fatal("Forget left the program running")
	}
}

// A holder that goes away mid-run ends its client's stream as lost.
func TestALostHolderEndsItsClient(t *testing.T) {
	dir := shortDir(t)
	l := &Launcher{Dir: dir, InProcess: true}
	if err := l.Start(Spec{ID: "s-lost", Command: "/bin/sh", Args: []string{"-c", "sleep 60"}, Workdir: dir}, nil); err != nil {
		t.Fatal(err)
	}
	c, err := Dial(dir, "s-lost", nil)
	if err != nil {
		t.Fatal(err)
	}
	other, _ := Dial(dir, "s-lost", nil)
	other.Forget() // ends the program and the holder
	if _, err := c.Wait(); err != nil && err != ErrHolderLost {
		t.Fatalf("wait: %v", err)
	}
}

// A program that ignores SIGTERM (an interactive shell) is ended by Forget,
// and its holder goes at once, not at its next check.
func TestForgetEndsAProgramThatIgnoresTerm(t *testing.T) {
	dir := shortDir(t)
	l := &Launcher{Dir: dir, InProcess: true}
	if err := l.Start(Spec{ID: "s-term", Command: "/bin/sh", Args: []string{"-c", "trap '' TERM; while :; do sleep 1; done"}, Workdir: dir}, nil); err != nil {
		t.Fatal(err)
	}
	c, err := Dial(dir, "s-term", nil)
	if err != nil {
		t.Fatal(err)
	}
	c.Signal(false)
	time.Sleep(200 * time.Millisecond)
	start := time.Now()
	c.Forget()
	for Alive(dir, "s-term") && time.Since(start) < 10*time.Second {
		time.Sleep(20 * time.Millisecond)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("the holder took %v to go", took)
	}
}
