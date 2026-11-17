// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package terminal

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// Session states as presented in terminal tiles.
const (
	StateStarting = "starting"
	StateRunning  = "running"
	StateIdle     = "idle"
	StateComplete = "complete"
	StateFailed   = "failed"
)

// DefaultScrollback is the ring's size when none is given: host.toml
// [terminals] scrollback_kib defaults to 256 (§8a.4, the bridge's value).
const DefaultScrollback = 256 * 1024

// idleAfter is how long a session may produce no output before it stops
// being called running.
const idleAfter = 6 * time.Second

// Session is one live terminal: a pseudo-terminal, the ring of its output,
// and the readers attached to it.
type Session struct {
	mutex sync.RWMutex

	id      string
	command string
	args    []string

	terminal PTY
	buffer   *ring
	columns  uint16
	rows     uint16

	state         string
	exitCode      int
	lastError     string
	startedAt     time.Time
	lastOutputAt  time.Time
	endedAt       *time.Time
	stopRequested bool

	nextWatcher int
	watchers    map[int]chan []byte
	done        chan struct{}
	closeOnce   sync.Once

	scanner  statusScanner
	statuses statusBook
	// onChange is called, outside the lock, when the session ends or its
	// status changes.
	onChange func(*Session)
}

// SessionView is a snapshot of a session: a reader never holds a reference
// to live process state.
type SessionView struct {
	ID           string     `json:"id"`
	Command      string     `json:"command"`
	State        string     `json:"state"`
	ExitCode     *int       `json:"exitCode,omitempty"`
	LastError    string     `json:"lastError,omitempty"`
	Stopped      bool       `json:"stopped"`
	StartedAt    time.Time  `json:"startedAt"`
	LastOutputAt time.Time  `json:"lastOutputAt"`
	EndedAt      *time.Time `json:"endedAt,omitempty"`
	Columns      uint16     `json:"columns"`
	Rows         uint16     `json:"rows"`
	BytesOut     int64      `json:"bytesOut"`
	PID          int        `json:"pid,omitempty"`
	StatusView
}

// ErrSessionClosed is returned when input is written to a finished session.
var ErrSessionClosed = errors.New("session is no longer running")

// Start runs spec in a new pseudo-terminal as session id, keeping the last
// scrollbackBytes of output (DefaultScrollback when 0 or less), and pumps
// its output until the process exits; onChange, when not nil, is called
// when the program's status changes and once after the exit is recorded. A command that is not found is refused before
// anything starts.
func Start(id string, spec LaunchSpec, scrollbackBytes int, onChange func(*Session)) (*Session, error) {
	if _, found := LookupCommand(spec.Command); !found {
		return nil, fmt.Errorf("start %s: the command is not installed on this machine", spec.Command)
	}
	session, err := newSession(id, spec, scrollbackBytes)
	if err != nil {
		return nil, err
	}
	session.onChange = onChange
	go session.pump()
	return session, nil
}

// Attach makes a session of a pseudo-terminal that is already running
// elsewhere (a holder process): the same ring, readers and status as a
// session Start runs, reading p until it reports the end.
func Attach(id string, p PTY, spec LaunchSpec, scrollbackBytes int, onChange func(*Session)) *Session {
	session := makeSession(id, p, spec, scrollbackBytes)
	session.onChange = onChange
	go session.pump()
	return session
}

// SetStartedAt corrects when the session's process started (an attached
// session began before this process knew it).
func (session *Session) SetStartedAt(at time.Time) {
	session.mutex.Lock()
	session.startedAt = at
	session.mutex.Unlock()
}

func newSession(id string, spec LaunchSpec, scrollbackBytes int) (*Session, error) {
	terminal, err := StartPTY(spec)
	if err != nil {
		return nil, err
	}
	return makeSession(id, terminal, spec, scrollbackBytes), nil
}

func makeSession(id string, terminal PTY, spec LaunchSpec, scrollbackBytes int) *Session {
	columns, rows := spec.size()
	now := time.Now().UTC()
	session := &Session{
		id:           id,
		command:      spec.Command,
		args:         spec.Args,
		terminal:     terminal,
		buffer:       newRing(scrollbackBytes),
		columns:      columns,
		rows:         rows,
		state:        StateStarting,
		exitCode:     -1,
		startedAt:    now,
		lastOutputAt: now,
		watchers:     map[int]chan []byte{},
		done:         make(chan struct{}),
	}
	return session
}

// ID is the session's id.
func (session *Session) ID() string { return session.id }

// pump reads the pseudo-terminal until the child exits, retaining output for
// reattachment and fanning it out to live watchers.
func (session *Session) pump() {
	chunk := make([]byte, 32*1024)
	for {
		count, err := session.terminal.Read(chunk)
		if count > 0 {
			payload := make([]byte, count)
			copy(payload, chunk[:count])
			session.output(payload)
		}
		if err != nil {
			break
		}
	}

	exitCode, waitErr := session.terminal.Wait()
	session.terminal.Close()
	session.mutex.Lock()
	ended := time.Now().UTC()
	session.endedAt = &ended
	session.exitCode = exitCode
	session.statuses.reset(ended)
	switch {
	case session.stopRequested:
		// A process asked to stop exits non-zero by design. That is a
		// completed instruction, not a failure.
		session.state = StateComplete
		session.lastError = "stopped"
	case exitCode == 0:
		session.state = StateComplete
		session.lastError = ""
	default:
		session.state = StateFailed
		if waitErr != nil && session.lastError == "" {
			session.lastError = waitErr.Error()
		}
	}
	session.mutex.Unlock()

	session.closeWatchers()
	session.closeOnce.Do(func() { close(session.done) })
	if session.onChange != nil {
		session.onChange(session)
	}
}

// output retains payload in the ring and hands it to every watcher, under
// the one lock Watch takes for its snapshot and registration: each byte is
// in a watcher's snapshot or in its feed, exactly once. (The bridge wrote
// the ring outside that lock, so a byte arriving between Watch's snapshot
// and its registration was lost.) A watcher's cancel closes its channel
// under the same lock, so no send meets a closed channel; the sends never
// block.
func (session *Session) output(payload []byte) {
	if session.record(payload) && session.onChange != nil {
		session.onChange(session)
	}
}

// record is output under the lock; it reports whether the status changed.
func (session *Session) record(payload []byte) bool {
	session.mutex.Lock()
	defer session.mutex.Unlock()
	changed := false
	for _, r := range session.scanner.scan(payload) {
		changed = session.statuses.apply(r, time.Now().UTC()) || changed
	}
	session.buffer.Write(payload)
	session.lastOutputAt = time.Now().UTC()
	if session.state == StateStarting || session.state == StateIdle {
		session.state = StateRunning
	}
	for id, watcher := range session.watchers {
		select {
		case watcher <- payload:
		default:
			// A viewer that falls a full feed behind is let go rather than
			// stalling the process: its feed closes while the session runs,
			// and it reconnects for a fresh snapshot. (The bridge dropped the
			// chunk and kept the viewer, so it went on with a hole in its
			// output, an escape sequence cut in two, and never knew.)
			close(watcher)
			delete(session.watchers, id)
		}
	}
	return changed
}

// Report records a status report that reached the forge another way than
// the program's output (a hook calling the API). It means what the same
// report as OSC 7501 would; a finished session takes none.
func (session *Session) Report(r Report) (bool, error) {
	if !r.Validate() {
		return false, ErrBadReport
	}
	session.mutex.Lock()
	if session.finishedLocked() {
		session.mutex.Unlock()
		return false, ErrSessionClosed
	}
	changed := session.statuses.apply(r, time.Now().UTC())
	session.mutex.Unlock()
	if changed && session.onChange != nil {
		session.onChange(session)
	}
	return changed, nil
}

// StatusReplay is the session's status records as OSC 7501, for a reader
// that joins after reports have left the ring (a forge adopting a holder).
func (session *Session) StatusReplay() []byte {
	session.mutex.Lock()
	defer session.mutex.Unlock()
	return []byte(session.statuses.encodeAll())
}

// ErrBadReport is a status report with an unknown state or out-of-range fields.
var ErrBadReport = errors.New("not a status report: state must be idle, working, done, blocked, error or clear")

func (session *Session) closeWatchers() {
	session.mutex.Lock()
	defer session.mutex.Unlock()
	for id, watcher := range session.watchers {
		close(watcher)
		delete(session.watchers, id)
	}
}

// Watching is a reader's join to a session's output at an offset, an index
// into everything the session has produced since it started.
type Watching struct {
	// From is the offset of Output[0].
	From int64
	// Dropped is how many bytes between the offset asked for and From the
	// ring no longer holds: 0 when the reader joins exactly where it asked,
	// else the exact gap, so a caller can tell a gap from a joined stream.
	Dropped int64
	// Output is the retained output from From on.
	Output []byte
	// Feed carries everything after Output, each byte once. It closes when
	// the session ends, or earlier when the reader falls a full feed (512
	// chunks) behind and is let go.
	Feed <-chan []byte
	// Ended is set when the session had already ended: Output is the rest of
	// its output and Feed is closed.
	Ended bool
	// Cancel stops the feed; calling it after the feed closed is harmless.
	Cancel func()
}

// WatchFrom joins the session's output at offset (F-0023-T3e): the bytes the
// ring still holds from offset on, then a live feed of everything after
// them, taken under the one lock output writes under, so no byte falls
// between them and none is in both. A reader keeps its position, From +
// len(Output) + the bytes its feed delivered; when its feed closes it calls
// WatchFrom with that position, and gets the bytes it missed (or, if the
// ring has wrapped past them, Dropped says exactly how many are gone), until
// a join reports Ended. It need not read View().State, which could change
// between the feed closing and the read: a reader let go just before the
// session ended took the end for its own and lost the bytes after its feed.
func (session *Session) WatchFrom(offset int64) Watching {
	session.mutex.Lock()
	defer session.mutex.Unlock()
	// output writes the ring under this lock, so its snapshot and its count
	// agree: the snapshot's first byte is at written - len(snapshot)
	retained := session.buffer.Snapshot()
	end := session.buffer.Written()
	from := min(max(offset, end-int64(len(retained))), end)
	output := retained[from-(end-int64(len(retained))):]
	w := Watching{From: from, Output: output, Cancel: func() {}}
	if offset < from {
		w.Dropped = from - offset
	}
	if session.finishedLocked() {
		closed := make(chan []byte)
		close(closed)
		w.Feed, w.Ended = closed, true
		return w
	}
	session.nextWatcher++
	id := session.nextWatcher
	feed := make(chan []byte, 512)
	session.watchers[id] = feed
	w.Feed = feed
	w.Cancel = func() {
		session.mutex.Lock()
		defer session.mutex.Unlock()
		if existing, found := session.watchers[id]; found {
			delete(session.watchers, id)
			close(existing)
		}
	}
	return w
}

// Watch returns the retained output plus a live feed of everything after it
// (WatchFrom from the oldest byte the ring holds). The feed closes when the
// session ends, or earlier if the reader falls a full feed behind; a reader
// that must tell those apart, and rejoin where it stopped, uses WatchFrom.
func (session *Session) Watch() ([]byte, <-chan []byte, func()) {
	w := session.WatchFrom(0)
	return w.Output, w.Feed, w.Cancel
}

// Write sends input to the terminal.
func (session *Session) Write(input []byte) error {
	session.mutex.RLock()
	finished := session.finishedLocked()
	session.mutex.RUnlock()
	if finished {
		return ErrSessionClosed
	}
	if _, err := session.terminal.Write(input); err != nil {
		if errors.Is(err, io.EOF) {
			return ErrSessionClosed
		}
		return fmt.Errorf("write to %s: %w", session.id, err)
	}
	return nil
}

// Resize propagates a new window size to the child process.
func (session *Session) Resize(columns, rows uint16) error {
	if columns == 0 || rows == 0 {
		return nil
	}
	session.mutex.Lock()
	session.columns, session.rows = columns, rows
	finished := session.finishedLocked()
	session.mutex.Unlock()
	if finished {
		return nil
	}
	return session.terminal.Resize(columns, rows)
}

// Stop asks the child to exit; force escalates to an immediate kill.
func (session *Session) Stop(force bool) error {
	session.mutex.Lock()
	finished := session.finishedLocked()
	if !finished {
		session.stopRequested = true
	}
	session.mutex.Unlock()
	if finished {
		return nil
	}
	return session.terminal.Signal(force)
}

// Wait blocks until the session's process has exited, or timeout passes.
func (session *Session) Wait(timeout time.Duration) bool {
	select {
	case <-session.done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// Done is closed when the session's process has exited and its exit is
// recorded.
func (session *Session) Done() <-chan struct{} { return session.done }

func (session *Session) finishedLocked() bool {
	return session.state == StateComplete || session.state == StateFailed
}

// Launch returns the command and arguments this session was started with,
// kept apart so a later run can start the same process again.
func (session *Session) Launch() (string, []string) {
	session.mutex.RLock()
	defer session.mutex.RUnlock()
	return session.command, append([]string(nil), session.args...)
}

// View renders the session, settling the activity-derived idle transition
// at read time rather than on a timer.
func (session *Session) View() SessionView {
	session.mutex.Lock()
	if session.state == StateRunning && time.Since(session.lastOutputAt) > idleAfter {
		session.state = StateIdle
	}
	view := SessionView{
		ID:           session.id,
		Command:      session.displayCommandLocked(),
		State:        session.state,
		LastError:    session.lastError,
		Stopped:      session.stopRequested,
		StartedAt:    session.startedAt,
		LastOutputAt: session.lastOutputAt,
		EndedAt:      session.endedAt,
		Columns:      session.columns,
		Rows:         session.rows,
		StatusView:   session.statuses.view(session.lastOutputAt, time.Now().UTC()),
	}
	if session.endedAt != nil {
		code := session.exitCode
		view.ExitCode = &code
	}
	session.mutex.Unlock()

	view.BytesOut = session.buffer.Written()
	if session.terminal != nil {
		view.PID = session.terminal.PID()
	}
	return view
}

func (session *Session) displayCommandLocked() string {
	line := session.command
	for _, argument := range session.args {
		line += " " + argument
	}
	return line
}
