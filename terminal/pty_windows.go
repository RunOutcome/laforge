// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

//go:build windows

package terminal

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/UserExistsError/conpty"
	"golang.org/x/sys/windows"
)

// windowsTerminal is a process on a ConPTY pseudo-console. ConPTY keeps its
// output pipe open after the process exits, until the console is closed,
// and its Close frees the console, so it must run once: a watcher waits for
// the process, keeps its exit code, then closes the console, which ends the
// reader's Read; Wait gives the kept code; Close runs once however many
// callers ask (OUTC-431: a second Close freed the console again, and the
// server died of heap corruption at the first terminal.stop).
//
// Signal ends the process through process, a handle opened at start and
// closed only by Close (OUTC-443). Windows never gives a PID to a new
// process while a handle to the old one is open, so this handle always
// names the terminal's own process; opening by PID when Signal is called
// could reach a later process that reused the PID.
type windowsTerminal struct {
	pty       *conpty.ConPty
	exited    chan struct{} // closed once the process has exited
	code      uint32
	waitErr   error
	closeOnce sync.Once
	closeErr  error

	mu      sync.Mutex     // guards process against Close while Signal uses it
	process windows.Handle // PROCESS_TERMINATE|SYNCHRONIZE; 0 once Close has closed it
}

// openProcess opens a process by PID. StartPTY is its only caller, while
// conpty's own handle still holds the PID; tests count its calls.
var openProcess = windows.OpenProcess

// StartPTY launches a process attached to a new ConPTY pseudo-console.
func StartPTY(spec LaunchSpec) (PTY, error) {
	columns, rows := spec.size()
	commandLine := quoteWindowsCommand(append([]string{spec.Command}, spec.Args...))
	options := []conpty.ConPtyOption{conpty.ConPtyDimensions(int(columns), int(rows))}
	if spec.Workdir != "" {
		options = append(options, conpty.ConPtyWorkDir(spec.Workdir))
	}
	options = append(options, conpty.ConPtyEnv(spec.environment()))
	console, err := conpty.Start(commandLine, options...)
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", spec.Command, err)
	}
	// conpty holds the process's handle until its Close, so the PID is
	// still this process's here, even if it has already exited
	process, err := openProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, uint32(console.Pid()))
	if err != nil {
		console.Close()
		return nil, fmt.Errorf("start %s: open its process: %w", spec.Command, err)
	}
	terminal := &windowsTerminal{pty: console, exited: make(chan struct{}), process: process}
	go terminal.watch()
	return terminal, nil
}

// watch waits for the process, keeps its exit code, then closes the
// console so the reader sees the end. It is the only caller of
// ConPty.Wait, which reads a handle Close closes.
func (terminal *windowsTerminal) watch() {
	terminal.code, terminal.waitErr = terminal.pty.Wait(context.Background())
	close(terminal.exited)
	terminal.Close()
}

func (terminal *windowsTerminal) Read(buffer []byte) (int, error)  { return terminal.pty.Read(buffer) }
func (terminal *windowsTerminal) Write(buffer []byte) (int, error) { return terminal.pty.Write(buffer) }

// Close closes the console and its handles, and the process handle, once.
func (terminal *windowsTerminal) Close() error {
	terminal.closeOnce.Do(func() {
		terminal.closeErr = terminal.pty.Close()
		terminal.mu.Lock()
		if terminal.process != 0 {
			windows.CloseHandle(terminal.process)
			terminal.process = 0
		}
		terminal.mu.Unlock()
	})
	return terminal.closeErr
}

func (terminal *windowsTerminal) Resize(columns, rows uint16) error {
	select {
	case <-terminal.exited:
		return nil // the console is closed, or about to be
	default:
	}
	return terminal.pty.Resize(int(columns), int(rows))
}

// Wait gives the process's exit code once it has exited.
func (terminal *windowsTerminal) Wait() (int, error) {
	<-terminal.exited
	return int(terminal.code), terminal.waitErr
}

// Signal ends the process. Windows offers no graceful signal to an
// unrelated console process, so a request to stop and a forced one both
// terminate it (exit 1); the watcher then closes the console. It uses
// only the handle opened at start, never the PID (OUTC-443).
func (terminal *windowsTerminal) Signal(force bool) error {
	select {
	case <-terminal.exited:
		return nil
	default:
	}
	terminal.mu.Lock()
	defer terminal.mu.Unlock()
	if terminal.process == 0 {
		return nil // closed: the process has ended and its terminal with it
	}
	if err := windows.TerminateProcess(terminal.process, 1); err != nil {
		select {
		case <-terminal.exited:
			return nil // it exited on its own meanwhile
		default:
		}
		return fmt.Errorf("stop the terminal's process: %w", err)
	}
	return nil
}

func (terminal *windowsTerminal) PID() int { return 0 }

// quoteWindowsCommand builds a command line using the quoting rules the Windows
// C runtime parser expects.
func quoteWindowsCommand(parts []string) string {
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" && !strings.ContainsAny(part, " \t\"") {
			quoted = append(quoted, part)
			continue
		}
		var builder strings.Builder
		builder.WriteByte('"')
		backslashes := 0
		for index := 0; index < len(part); index++ {
			switch part[index] {
			case '\\':
				backslashes++
			case '"':
				builder.WriteString(strings.Repeat(`\`, backslashes*2+1))
				builder.WriteByte('"')
				backslashes = 0
			default:
				builder.WriteString(strings.Repeat(`\`, backslashes))
				builder.WriteByte(part[index])
				backslashes = 0
			}
		}
		builder.WriteString(strings.Repeat(`\`, backslashes*2))
		builder.WriteByte('"')
		quoted = append(quoted, builder.String())
	}
	return strings.Join(quoted, " ")
}
