// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

//go:build windows

package terminal

import (
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// stubArg makes this test binary the ConPTY's process: "exit N", "sleep N".
const stubArg = "outcome-test-stub"

func TestMain(m *testing.M) {
	if len(os.Args) == 4 && os.Args[1] == stubArg {
		n, _ := strconv.Atoi(os.Args[3])
		if os.Args[2] == "sleep" {
			time.Sleep(time.Duration(n) * time.Second)
			n = 0
		}
		os.Exit(n)
	}
	os.Exit(m.Run())
}

func startStub(t *testing.T, args ...string) PTY {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p, err := StartPTY(LaunchSpec{Command: exe, Args: append([]string{stubArg}, args...)})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// readToEnd reads p until its Read fails, within d.
func readToEnd(t *testing.T, p PTY, d time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() { io.Copy(io.Discard, p); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("Read did not end within %s of the process ending", d)
	}
}

// OUTC-431: on ConPTY a process that exits on its own gives its exit code
// to Wait, and the reader's Read ends (ConPTY keeps the pipe open until the
// console is closed); Close after that, and again, is safe.
func TestConPTYExitEndsTheReadAndGivesTheCode(t *testing.T) {
	p := startStub(t, "exit", "3")
	readToEnd(t, p, 10*time.Second)
	if code, err := p.Wait(); code != 3 || err != nil {
		t.Errorf("Wait = %d %v, want 3", code, err)
	}
	p.Close()
	p.Close()
}

// OUTC-431: Signal ends a process that would run on, the read ends, Wait
// gives its code, and the Close that follows (the session's, after the
// watcher's) frees nothing twice: before, the second Close freed the
// console again and the process died of heap corruption.
func TestConPTYSignalThenCloseTwice(t *testing.T) {
	p := startStub(t, "sleep", "60")
	time.Sleep(300 * time.Millisecond)
	if err := p.Signal(false); err != nil {
		t.Fatal(err)
	}
	readToEnd(t, p, 10*time.Second)
	if code, _ := p.Wait(); code != 1 {
		t.Errorf("Wait after Signal = %d, want 1 (terminated)", code)
	}
	p.Close()
	p.Close()
	if err := p.Signal(true); err != nil {
		t.Errorf("Signal after the end = %v, want nil", err)
	}
	if err := p.Resize(80, 24); err != nil {
		t.Errorf("Resize after the end = %v, want nil", err)
	}
}

// OUTC-443 @ac-1: Signal uses the handle StartPTY opened, never the PID. A
// process that has exited, with a second process started after it (which
// may be given its PID once the first's handles close), is signalled: the
// call answers nil, the second process is still running, and Signal opened
// no process by PID. The handle is closed once, by Close.
func TestConPTYSignalAfterExitTouchesNoOtherProcess(t *testing.T) {
	var opens atomic.Int32
	start := openProcess
	openProcess = func(access uint32, inherit bool, pid uint32) (windows.Handle, error) {
		opens.Add(1)
		return start(access, inherit, pid)
	}
	t.Cleanup(func() { openProcess = start })

	p := startStub(t, "exit", "0")
	if n := opens.Load(); n != 1 {
		t.Fatalf("StartPTY opened %d processes by PID, want 1 (its own, at start)", n)
	}
	readToEnd(t, p, 10*time.Second)
	if code, err := p.Wait(); code != 0 || err != nil {
		t.Fatalf("Wait = %d %v, want 0", code, err)
	}
	p.Close() // the watcher's Close has run or runs now: its handles are gone

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	other := exec.Command(exe, stubArg, "sleep", "30")
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	ended := make(chan error, 1)
	go func() { ended <- other.Wait() }()
	t.Cleanup(func() { other.Process.Kill(); <-ended })

	for _, force := range []bool{false, true} {
		if err := p.Signal(force); err != nil {
			t.Errorf("Signal(%v) after the process exited = %v, want nil", force, err)
		}
	}
	if n := opens.Load(); n != 1 {
		t.Errorf("after Signal, %d processes opened by PID, want only StartPTY's 1", n)
	}
	select {
	case err := <-ended:
		t.Errorf("the other process (pid %d) ended after the exited terminal's Signal: %v", other.Process.Pid, err)
	case <-time.After(time.Second):
	}
}

// OUTC-443 @ac-1: a running process's Signal ends it through the handle
// opened at start, with no OpenProcess by PID at the time of the call.
func TestConPTYSignalUsesTheHandleFromStart(t *testing.T) {
	p := startStub(t, "sleep", "60")
	start := openProcess
	openProcess = func(uint32, bool, uint32) (windows.Handle, error) {
		t.Error("Signal opened a process by PID")
		return 0, windows.ERROR_ACCESS_DENIED
	}
	t.Cleanup(func() { openProcess = start })
	time.Sleep(300 * time.Millisecond)
	if err := p.Signal(false); err != nil {
		t.Fatalf("Signal = %v", err)
	}
	readToEnd(t, p, 10*time.Second)
	if code, _ := p.Wait(); code != 1 {
		t.Errorf("Wait after Signal = %d, want 1 (terminated)", code)
	}
	p.Close()
}
