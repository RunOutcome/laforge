// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package terminal

import (
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// Ported from the bridge (bridge_test.go TestRingBufferRetainsTheMostRecentOutput).
func TestRingBufferRetainsTheMostRecentOutput(t *testing.T) {
	buffer := newRing(8)
	buffer.Write([]byte("abcdef"))
	buffer.Write([]byte("ghij"))
	if got := string(buffer.Snapshot()); got != "cdefghij" {
		t.Fatalf("snapshot = %q, want %q", got, "cdefghij")
	}
	if buffer.Written() != 10 {
		t.Fatalf("written = %d, want 10", buffer.Written())
	}

	// A single write larger than the buffer keeps only its tail.
	buffer.Write([]byte("0123456789"))
	if got := string(buffer.Snapshot()); got != "23456789" {
		t.Fatalf("snapshot after overflow = %q, want %q", got, "23456789")
	}
}

// The default ring is the 256 KiB of host.toml [terminals] scrollback_kib.
func TestRingDefaultsTo256KiB(t *testing.T) {
	if r := newRing(0); r.size != 256*1024 || DefaultScrollback != 256*1024 {
		t.Fatalf("default ring %d, DefaultScrollback %d", r.size, DefaultScrollback)
	}
}

// Ported from the bridge (provisional_test.go
// TestProvisionalLaunchDropsInheritedAndConfiguredAddress), as an
// allowlist: the server's secrets and its own OUTCOME_* identity are not
// inherited; PATH, HOME, LC_* are; TERM defaults; the spec's keys are
// kept; the parent's environment is not changed.
func TestLaunchInheritsOnlyTheAllowlist(t *testing.T) {
	t.Setenv("FOO_SECRET", "x")
	t.Setenv("GH_TOKEN", "y")
	t.Setenv("OUTCOME_TOKEN", "server-token")
	t.Setenv("LC_ALL", "C")
	t.Setenv("TERM", "")
	os.Unsetenv("TERM")
	environment := launchEnvironment([]string{"OUTCOME_WS=book-club", "OUTCOME_ITEM=chap-0001", "PROFILE_FLAG=1"})
	got := map[string][]string{}
	for _, entry := range environment {
		k, v, _ := strings.Cut(entry, "=")
		if runtime.GOOS == "windows" {
			k = strings.ToUpper(k) // Windows names are case-insensitive: Path is PATH
		}
		got[k] = append(got[k], v)
	}
	for _, k := range []string{"FOO_SECRET", "GH_TOKEN", "OUTCOME_TOKEN"} {
		if len(got[k]) != 0 {
			t.Errorf("%s was inherited: %v", k, got[k])
		}
	}
	for k, want := range map[string]string{"OUTCOME_WS": "book-club", "OUTCOME_ITEM": "chap-0001", "PROFILE_FLAG": "1", "LC_ALL": "C", "TERM": defaultTerm, "PATH": os.Getenv("PATH"), "HOME": os.Getenv("HOME")} {
		if strings.Join(got[k], ",") != want {
			t.Errorf("%s = %v, want %q", k, got[k], want)
		}
	}
	if os.Getenv("FOO_SECRET") != "x" {
		t.Error("parent environment mutated")
	}
}

// On Windows keys match case-insensitively and SystemRoot and friends are
// kept; elsewhere they are not, and keys are exact.
func TestAllowedByPlatform(t *testing.T) {
	for _, c := range []struct {
		key, goos string
		want      bool
	}{
		{"Path", "windows", true}, {"SystemRoot", "windows", true}, {"GH_TOKEN", "windows", false},
		{"PATH", "linux", true}, {"Path", "linux", false}, {"SystemRoot", "darwin", false},
		{"LC_CTYPE", "darwin", true}, {"AWS_SECRET_ACCESS_KEY", "linux", false}, {"OUTCOME_TOKEN", "linux", false},
	} {
		if got := allowed(c.key, c.goos); got != c.want {
			t.Errorf("allowed(%s, %s) = %v, want %v", c.key, c.goos, got, c.want)
		}
	}
}

// The child itself sees the allowlist: env run in a session has PATH, HOME
// and the spec's keys, and neither FOO_SECRET nor GH_TOKEN.
func TestTheChildSeesNoServerSecrets(t *testing.T) {
	posix(t)
	t.Setenv("FOO_SECRET", "x")
	t.Setenv("GH_TOKEN", "y")
	session, err := Start("term-0005", LaunchSpec{Command: "env", Env: []string{"OUTCOME_WS=book-club"}}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := read(t, session)
	keys := map[string]bool{}
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		k, _, _ := strings.Cut(line, "=")
		keys[k] = true
	}
	if keys["FOO_SECRET"] || keys["GH_TOKEN"] || !keys["PATH"] || !keys["HOME"] || !keys["OUTCOME_WS"] {
		t.Fatalf("child env %q", out)
	}
}

func posix(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("this check uses a POSIX shell")
	}
}

func start(t *testing.T, script string, scrollback int) *Session {
	t.Helper()
	session, err := Start("term-0001", LaunchSpec{Command: "/bin/sh", Args: []string{"-c", script}, Columns: 80, Rows: 24}, scrollback, nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() {
		session.Stop(true)
		session.Wait(5 * time.Second)
	})
	return session
}

// read collects a session's whole output as a browser would: it joins at
// offset 0 and, whenever its feed closes, joins again where it stopped,
// until a join reports the session Ended (F-0023-T3e). A feed closes both
// when the session ends and when the reader falls a full feed behind and is
// let go; the reader does not guess which from View().State, which could
// change in between. The ring holds these tests' output whole, so a join
// that reports dropped bytes fails.
func read(t *testing.T, session *Session) string {
	t.Helper()
	deadline := time.After(10 * time.Second)
	var seen strings.Builder
	for {
		w := session.WatchFrom(int64(seen.Len()))
		if w.Dropped != 0 || w.From != int64(seen.Len()) {
			w.Cancel()
			t.Fatalf("rejoining at %d: the ring dropped %d bytes, output from %d", seen.Len(), w.Dropped, w.From)
		}
		seen.Write(w.Output)
	collect:
		for {
			select {
			case chunk, open := <-w.Feed:
				if !open {
					break collect
				}
				seen.Write(chunk)
			case <-deadline:
				w.Cancel()
				t.Fatalf("the feed never closed; saw %q", seen.String())
			}
		}
		w.Cancel()
		if w.Ended {
			return seen.String()
		}
	}
}

// A command printing 5 lines is read back in order, and its exit is reported.
func TestFiveLinesAreReadBackInOrder(t *testing.T) {
	posix(t)
	session := start(t, "sleep 0.2; for i in 1 2 3 4 5; do echo line-$i; done", 0)
	got := strings.ReplaceAll(read(t, session), "\r\n", "\n")
	if got != "line-1\nline-2\nline-3\nline-4\nline-5\n" {
		t.Fatalf("output %q", got)
	}
	if !session.Wait(5 * time.Second) {
		t.Fatal("no exit")
	}
	if v := session.View(); v.State != StateComplete || v.ExitCode == nil || *v.ExitCode != 0 {
		t.Fatalf("view %+v", v)
	}
}

// A reader attaching after the output gets the ring first, then the feed
// closes at exit; a reader attaching mid-stream gets every byte exactly
// once across its snapshot and its feed.
func TestALateReaderGetsTheRingFirst(t *testing.T) {
	posix(t)
	session := start(t, "echo early; sleep 0.5; echo late", 0)
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(string(session.buffer.Snapshot()), "early") {
		if time.Now().After(deadline) {
			t.Fatal("no early output")
		}
		time.Sleep(10 * time.Millisecond)
	}
	snapshot, feed, cancel := session.Watch()
	defer cancel()
	if !strings.HasPrefix(string(snapshot), "early") || strings.Contains(string(snapshot), "late") {
		t.Fatalf("snapshot %q, want the early output only", snapshot)
	}
	var live string
	for chunk := range feed {
		live += string(chunk)
	}
	if strings.TrimSpace(live) != "late" {
		t.Fatalf("live %q, want late", live)
	}
	after, closed, _ := session.Watch()
	if _, open := <-closed; open || strings.ReplaceAll(string(after), "\r\n", "\n") != "early\nlate\n" {
		t.Fatalf("after exit: snapshot %q, feed open %v", after, open)
	}
}

func TestAMidStreamReaderGetsEveryByteOnce(t *testing.T) {
	posix(t)
	session := start(t, "i=1; while [ $i -le 3000 ]; do echo n$i; i=$((i+1)); done", 0)
	time.Sleep(5 * time.Millisecond)
	raw := read(t, session)
	if !session.Wait(10 * time.Second) {
		t.Fatal("no exit")
	}
	// Exactly once: the reader's snapshot and feed are byte for byte what
	// the pseudo-terminal produced, which the ring holds whole (17 KB of
	// 256 KB).
	if produced := string(session.buffer.Snapshot()); raw != produced {
		t.Fatalf("snapshot+feed has %d bytes, the terminal produced %d (lost or doubled output); first difference at %d", len(raw), len(produced), firstDiff(raw, produced))
	}
	// The lines are the program's. The terminal's output processing turns
	// each \n into \r\n, and on macOS it now and then emits the \r twice
	// when its output queue fills mid-conversion (seen in the feed of a
	// reader attached before any output, so not the handoff): any run of
	// \r before \n is one line end.
	got := crlf.ReplaceAllString(raw, "\n")
	var want strings.Builder
	for i := 1; i <= 3000; i++ {
		fmt.Fprintf(&want, "n%d\n", i)
	}
	if got != want.String() {
		t.Fatalf("lines are %d bytes, want %d; first difference at %d", len(got), want.Len(), firstDiff(got, want.String()))
	}
}

// A reader that falls a full feed behind is let go: its feed closes while
// the session runs, and Watching again gives the
// output from the ring with nothing missing.
func TestAReaderThatFallsBehindIsLetGo(t *testing.T) {
	session := &Session{buffer: newRing(1 << 20), state: StateRunning, watchers: map[int]chan []byte{}, done: make(chan struct{})}
	_, slow, cancelSlow := session.Watch()
	defer cancelSlow()
	var stream strings.Builder
	for i := range 600 {
		chunk := fmt.Sprintf("c%04d;", i)
		stream.WriteString(chunk)
		session.output([]byte(chunk))
	}
	received := 0
	for range slow {
		received++
	}
	if received != 512 {
		t.Fatalf("slow reader got %d chunks before its feed closed, want 512", received)
	}
	if v := session.View(); v.State != StateRunning {
		t.Fatalf("session state %s after letting a reader go", v.State)
	}
	cancelSlow() // cancelling a let-go reader is harmless
	snapshot, _, cancel := session.Watch()
	defer cancel()
	if string(snapshot) != stream.String() {
		t.Fatalf("the new snapshot has %d bytes, want %d", len(snapshot), stream.Len())
	}
}

var crlf = regexp.MustCompile(`\r+\n`)

// Readers that join at every point of a stream each get it exactly once:
// the snapshot up to the join and the feed from there, with nothing
// between them and nothing in both. The writer is the session's own
// output path, without a pseudo-terminal, so the stream is exactly known.
func TestReadersJoiningAtManyOffsetsGetEveryByteOnce(t *testing.T) {
	for _, ringSize := range []int{1 << 20, 64} {
		session := &Session{buffer: newRing(ringSize), state: StateRunning, watchers: map[int]chan []byte{}, done: make(chan struct{})}
		const chunks = 400 // under the feed's 512, so no reader is ever dropped as slow
		var full strings.Builder
		for i := range chunks {
			fmt.Fprintf(&full, "c%04d;", i)
		}
		type result struct {
			joinedAfter int
			data        string
		}
		results := make(chan result, chunks)
		var readers sync.WaitGroup
		written := make(chan int, chunks)
		go func() {
			for i := range chunks {
				session.output([]byte(fmt.Sprintf("c%04d;", i)))
				written <- i
			}
			close(written)
		}()
		for i := range written {
			if i%3 != 0 {
				continue
			}
			snapshot, feed, _ := session.Watch()
			readers.Add(1)
			go func(after int) {
				defer readers.Done()
				data := string(snapshot)
				for chunk := range feed {
					data += string(chunk)
				}
				results <- result{after, data}
			}(i)
		}
		session.closeWatchers()
		readers.Wait()
		close(results)
		want, n := full.String(), 0
		for r := range results {
			n++
			if ringSize >= len(want) {
				if r.data != want {
					t.Fatalf("ring %d, reader after chunk %d: %d bytes, want %d; first difference at %d", ringSize, r.joinedAfter, len(r.data), len(want), firstDiff(r.data, want))
				}
				continue
			}
			// A small ring keeps only the newest bytes, so the snapshot
			// starts wherever the ring did at the join: the reader has a
			// contiguous tail of the stream, at least the ring's worth,
			// with no byte missing or doubled after it starts.
			if !strings.HasSuffix(want, r.data) || len(r.data) < ringSize {
				t.Fatalf("ring %d, reader after chunk %d: %q is not a whole tail of the stream", ringSize, r.joinedAfter, r.data)
			}
		}
		if n != (chunks+2)/3 {
			t.Fatalf("ring %d: %d readers, want %d", ringSize, n, (chunks+2)/3)
		}
	}
}

func firstDiff(a, b string) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}

// The ring keeps the newest bytes when the output overflows it.
func TestTheRingKeepsTheNewestBytes(t *testing.T) {
	posix(t)
	session := start(t, "i=1; while [ $i -le 500 ]; do echo row-$i; i=$((i+1)); done", 64)
	if !session.Wait(10 * time.Second) {
		t.Fatal("no exit")
	}
	snapshot := string(session.buffer.Snapshot())
	if len(snapshot) != 64 || !strings.HasSuffix(snapshot, "row-500\r\n") || strings.Contains(snapshot, "row-1\r") {
		t.Fatalf("ring %q", snapshot)
	}
	if v := session.View(); v.BytesOut <= 64 {
		t.Fatalf("bytesOut %d", v.BytesOut)
	}
}

// Resize reaches the child: stty size reports the new size.
func TestResizeReachesTheChild(t *testing.T) {
	posix(t)
	session := start(t, "stty size; read go; stty size", 0)
	_, feed, cancel := session.Watch()
	defer cancel()
	seen := ""
	wait := func(want string) {
		t.Helper()
		deadline := time.After(10 * time.Second)
		for !strings.Contains(seen, want) {
			select {
			case chunk := <-feed:
				seen += string(chunk)
			case <-deadline:
				t.Fatalf("never saw %q in %q", want, seen)
			}
		}
	}
	wait("24 80")
	if err := session.Resize(132, 40); err != nil {
		t.Fatal(err)
	}
	if err := session.Write([]byte("go\r")); err != nil {
		t.Fatal(err)
	}
	wait("40 132")
	if v := session.View(); v.Columns != 132 || v.Rows != 40 {
		t.Fatalf("view size %dx%d", v.Columns, v.Rows)
	}
}

// Input written to the session reaches the program (the bridge's
// TestTypedTextReachesTheProgramInTheSession, through Write: typing is not
// ported).
func TestWrittenInputReachesTheProgram(t *testing.T) {
	posix(t)
	session, err := Start("term-0002", LaunchSpec{Command: "/bin/sh", Args: []string{"-i"}}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Stop(true); session.Wait(5 * time.Second) })
	_, feed, cancel := session.Watch()
	defer cancel()
	if err := session.Write([]byte("echo written-and-run\r")); err != nil {
		t.Fatal(err)
	}
	var seen string
	deadline := time.After(10 * time.Second)
	for strings.Count(seen, "written-and-run") < 2 {
		select {
		case chunk, open := <-feed:
			if !open {
				t.Fatalf("ended early; saw %q", seen)
			}
			seen += string(chunk)
		case <-deadline:
			t.Fatalf("never ran; saw %q", seen)
		}
	}
}

// Ported from the bridge (TestAStoppedSessionIsCompleteRatherThanFailed):
// a stopped session is complete, a non-zero exit is failed with its code,
// and onExit is called once; writing after the exit is ErrSessionClosed.
func TestAStoppedSessionIsCompleteRatherThanFailed(t *testing.T) {
	posix(t)
	session := start(t, "sleep 60", 0)
	if err := session.Stop(false); err != nil {
		t.Fatalf("stop session: %v", err)
	}
	if !session.Wait(5 * time.Second) {
		t.Fatal("the session did not exit after a graceful stop")
	}
	if v := session.View(); v.State != StateComplete || !v.Stopped {
		t.Fatalf("state after a stop = %q stopped %v, want %q", v.State, v.Stopped, StateComplete)
	}
	exits := make(chan *Session, 2)
	failing, err := Start("term-0003", LaunchSpec{Command: "/bin/sh", Args: []string{"-c", "exit 3"}}, 0, func(s *Session) { exits <- s })
	if err != nil {
		t.Fatal(err)
	}
	if !failing.Wait(5 * time.Second) {
		t.Fatal("the failing session did not exit")
	}
	select {
	case s := <-exits:
		if s != failing {
			t.Fatal("onExit got another session")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("onExit not called")
	}
	if v := failing.View(); v.State != StateFailed || v.ExitCode == nil || *v.ExitCode != 3 || v.Stopped {
		t.Fatalf("view after exit 3 = %+v", v)
	}
	if err := failing.Write([]byte("x")); err != ErrSessionClosed {
		t.Fatalf("write after exit = %v", err)
	}
	if len(exits) != 0 {
		t.Fatal("onExit called twice")
	}
}

// Ported from the bridge (TestStartSessionRefusesAMissingCommand).
func TestStartRefusesAMissingCommand(t *testing.T) {
	_, err := Start("term-0004", LaunchSpec{Command: "outcome-command-that-does-not-exist"}, 0, nil)
	if err == nil {
		t.Fatal("a session started with a command that is not installed")
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("error = %v, want it to explain the command is not installed", err)
	}
	if _, ok := LookupCommand(""); ok {
		t.Fatal("an empty command was found")
	}
}

// A child that writes and exits at once is read whole by a reader that
// attaches only after the exit (F-0023-T3d). On macOS a pseudo-terminal's
// last slave close discards output the master has not read yet, so under
// load the child's exit took its output with it and the ring stayed empty;
// the server holds its own slave open until the child is reaped.
func TestAFastExitingChildIsReadWholeAfterItExits(t *testing.T) {
	posix(t)
	want, args := "", []string{"line-%s\n"}
	for i := 1; i <= 40; i++ {
		want += fmt.Sprintf("line-%02d\n", i)
		args = append(args, fmt.Sprintf("%02d", i))
	}
	for run := 0; run < 20; run++ {
		// printf itself, not a shell: it writes once and exits at once
		session, err := Start("term-0006", LaunchSpec{Command: "printf", Args: args}, 0, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !session.Wait(5 * time.Second) {
			t.Fatal("no exit")
		}
		snapshot, feed, cancel := session.Watch()
		if _, open := <-feed; open {
			t.Fatal("a finished session's feed is open")
		}
		cancel()
		if got := strings.ReplaceAll(string(snapshot), "\r\n", "\n"); got != want {
			t.Fatalf("run %d: after exit the ring holds %q, want 40 lines", run, got)
		}
	}
}

// Sessions that exit, are stopped, or fail to start leave no descriptor
// open (F-0023-T3d): the server's slave is closed on every path, and each
// session's pump ends once its child is reaped.
func TestSessionsLeaveNoDescriptorsOpen(t *testing.T) {
	posix(t)
	// fcntl reads the descriptor table itself: a slave the kernel revoked
	// when its session ended is invisible to fstat and /dev/fd, but open
	open := openDescriptors
	before := open()
	for i := 0; i < 30; i++ {
		exits, err := Start("term-0007", LaunchSpec{Command: "printf", Args: []string{"x"}}, 0, nil)
		if err != nil {
			t.Fatal(err)
		}
		stopped, err := Start("term-0008", LaunchSpec{Command: "/bin/sh", Args: []string{"-c", "sleep 30"}}, 0, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := stopped.Stop(true); err != nil {
			t.Fatal(err)
		}
		if _, err := Start("term-0009", LaunchSpec{Command: "/no/such/command"}, 0, nil); err == nil {
			t.Fatal("a missing command started")
		}
		for _, s := range []*Session{exits, stopped} {
			select {
			case <-s.Done():
			case <-time.After(5 * time.Second):
				t.Fatalf("session %s did not end", s.ID())
			}
		}
	}
	if after := open(); after != before {
		t.Fatalf("%d descriptors open before 90 sessions, %d after", before, after)
	}
}

// fakeSession is a session without a process, whose output is written by
// hand, so readers can be made to fall behind exactly.
func fakeSession(ringSize int) *Session {
	return &Session{buffer: newRing(ringSize), state: StateRunning, watchers: map[int]chan []byte{}, done: make(chan struct{})}
}

// end finishes a fakeSession as pump does: its state, then its feeds.
func (session *Session) end() {
	session.mutex.Lock()
	session.state = StateComplete
	session.mutex.Unlock()
	session.closeWatchers()
	session.closeOnce.Do(func() { close(session.done) })
}

// drain reads a feed to its close and returns what it carried.
func drain(feed <-chan []byte) string {
	var got strings.Builder
	for chunk := range feed {
		got.Write(chunk)
	}
	return got.String()
}

// A reader let go just before the session ends rejoins where its feed
// stopped and gets the rest (F-0023-T3e). This is the mid-stream test's
// loss made exact: the old reader saw its feed close, read View().State as
// complete, and returned 3072 of the 3600 bytes.
func TestALetGoReaderRejoinsWhereItStoppedAfterTheEnd(t *testing.T) {
	session := fakeSession(1 << 20)
	first := session.WatchFrom(0)
	var stream strings.Builder
	for i := range 600 {
		chunk := fmt.Sprintf("c%04d;", i)
		stream.WriteString(chunk)
		session.output([]byte(chunk))
	}
	session.end()
	got := string(first.Output) + drain(first.Feed)
	if len(got) != 512*6 || first.Ended {
		t.Fatalf("the let-go feed carried %d bytes (ended %v), want 512 chunks of 6 before it closed", len(got), first.Ended)
	}
	if v := session.View(); v.State != StateComplete {
		t.Fatalf("state %s, want the session complete before the reader rejoins", v.State)
	}
	again := session.WatchFrom(int64(len(got)))
	if again.Dropped != 0 || again.From != int64(len(got)) || !again.Ended {
		t.Fatalf("rejoin at %d: from %d, dropped %d, ended %v; want from %d, none dropped, ended", len(got), again.From, again.Dropped, again.Ended, len(got))
	}
	if _, open := <-again.Feed; open {
		t.Fatal("an ended session's feed is open")
	}
	if got += string(again.Output); got != stream.String() {
		t.Fatalf("after rejoining the reader has %d bytes, want %d", len(got), stream.Len())
	}
}

// A reader let go while the session runs rejoins where it stopped: the
// ring's bytes after its position, then the live feed, each byte once.
func TestALetGoReaderRejoinsWhereItStoppedMidRun(t *testing.T) {
	session := fakeSession(1 << 20)
	first := session.WatchFrom(0)
	var stream strings.Builder
	write := func(from, to int) {
		for i := from; i < to; i++ {
			chunk := fmt.Sprintf("c%04d;", i)
			stream.WriteString(chunk)
			session.output([]byte(chunk))
		}
	}
	write(0, 600)
	got := drain(first.Feed)
	again := session.WatchFrom(int64(len(got)))
	if again.Dropped != 0 || again.From != int64(len(got)) || again.Ended {
		t.Fatalf("rejoin at %d: from %d, dropped %d, ended %v", len(got), again.From, again.Dropped, again.Ended)
	}
	got += string(again.Output)
	write(600, 700)
	session.end()
	if got += drain(again.Feed); got != stream.String() {
		t.Fatalf("after rejoining the reader has %d bytes, want %d", len(got), stream.Len())
	}
}

// A reader that rejoins after the ring has wrapped past its position is
// told the exact gap: Dropped is the bytes between its position and the
// ring's oldest, and Output starts at that oldest byte.
func TestARejoinPastTheRingReportsTheExactGap(t *testing.T) {
	session := fakeSession(60)
	first := session.WatchFrom(0)
	var stream strings.Builder
	for i := range 600 {
		chunk := fmt.Sprintf("c%04d;", i)
		stream.WriteString(chunk)
		session.output([]byte(chunk))
	}
	position := int64(len(drain(first.Feed)))
	again := session.WatchFrom(position)
	defer again.Cancel()
	all := stream.String()
	oldest := int64(len(all) - 60)
	if again.From != oldest || again.Dropped != oldest-position || string(again.Output) != all[oldest:] {
		t.Fatalf("rejoin at %d: from %d, dropped %d, %d bytes; want from %d, dropped %d, the last 60 bytes", position, again.From, again.Dropped, len(again.Output), oldest, oldest-position)
	}
	// joining at 0 on a wrapped ring reports everything before the oldest
	if w := session.WatchFrom(0); w.From != oldest || w.Dropped != oldest {
		t.Fatalf("join at 0: from %d, dropped %d, want %d and %d", w.From, w.Dropped, oldest, oldest)
	} else {
		w.Cancel()
	}
	// a position past the end gives the end and no bytes
	if w := session.WatchFrom(int64(len(all)) + 10); w.From != int64(len(all)) || len(w.Output) != 0 || w.Dropped != 0 {
		t.Fatalf("join past the end: from %d, %d bytes, dropped %d", w.From, len(w.Output), w.Dropped)
	} else {
		w.Cancel()
	}
}
