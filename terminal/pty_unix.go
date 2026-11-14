// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

//go:build !windows

package terminal

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
)

type unixTerminal struct {
	file    *os.File
	command *exec.Cmd
	exited  chan struct{} // closed once the child is reaped and the slave closed
	code    int
	err     error
}

// StartPTY launches a process attached to a new pseudo-terminal.
func StartPTY(spec LaunchSpec) (PTY, error) {
	columns, rows := spec.size()
	command := exec.Command(spec.Command, spec.Args...)
	command.Dir = spec.Workdir
	command.Env = spec.environment()
	// Put the child in its own process group so that stopping a session cannot
	// signal the Outcome server itself.
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}

	// The server keeps its own slave descriptor until the child is reaped
	// (F-0023-T3d). pty.StartWithSize closed it at start, so the child's exit
	// was the slave's last close, and macOS then discards output the master
	// has not read: under load a fast child such as env lost all of it.
	// Holding the slave, the child's output stays readable; it is closed
	// after Wait, on every path, and the pump reads to EOF as before.
	file, tty, err := pty.Open()
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", spec.Command, err)
	}
	if err := pty.Setsize(file, &pty.Winsize{Cols: columns, Rows: rows}); err != nil {
		file.Close()
		tty.Close()
		return nil, fmt.Errorf("start %s: %w", spec.Command, err)
	}
	command.Stdin, command.Stdout, command.Stderr = tty, tty, tty
	if err := command.Start(); err != nil {
		file.Close()
		tty.Close()
		return nil, fmt.Errorf("start %s: %w", spec.Command, err)
	}
	terminal := &unixTerminal{file: file, command: command, exited: make(chan struct{})}
	go func() {
		terminal.err = command.Wait()
		terminal.code = -1
		if command.ProcessState != nil {
			terminal.code = command.ProcessState.ExitCode()
		}
		tty.Close()
		close(terminal.exited)
	}()
	return terminal, nil
}

func (terminal *unixTerminal) Read(buffer []byte) (int, error)  { return terminal.file.Read(buffer) }
func (terminal *unixTerminal) Write(buffer []byte) (int, error) { return terminal.file.Write(buffer) }
func (terminal *unixTerminal) Close() error                     { return terminal.file.Close() }

func (terminal *unixTerminal) Resize(columns, rows uint16) error {
	return pty.Setsize(terminal.file, &pty.Winsize{Cols: columns, Rows: rows})
}

func (terminal *unixTerminal) Wait() (int, error) {
	<-terminal.exited
	return terminal.code, terminal.err
}

func (terminal *unixTerminal) Signal(force bool) error {
	if terminal.command.Process == nil {
		return nil
	}
	signal := syscall.SIGTERM
	if force {
		signal = syscall.SIGKILL
	}
	// Signal the whole process group: an agent CLI usually has children.
	if group, err := syscall.Getpgid(terminal.command.Process.Pid); err == nil {
		if err := syscall.Kill(-group, signal); err == nil {
			return nil
		}
	}
	return terminal.command.Process.Signal(signal)
}

func (terminal *unixTerminal) PID() int {
	if terminal.command.Process == nil {
		return 0
	}
	return terminal.command.Process.Pid
}
