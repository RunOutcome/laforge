// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// Package terminal runs a command in a pseudo-terminal, keeps its output in
// a ring buffer and lets readers attach (platform-topology.md §8a.4). It is
// the earlier bridge's PTY, LaunchSpec, ring and Session, ported; the
// bridge's message bus, typing into a PTY and layout are not.
package terminal

import (
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// PTY is the platform-neutral pseudo-terminal contract. Unix uses a PTY pair,
// Windows uses ConPTY; nothing above this interface branches on the OS.
type PTY interface {
	io.ReadWriteCloser
	// Resize informs the child process of a new window size.
	Resize(columns, rows uint16) error
	// Wait blocks until the child exits and reports its exit code.
	Wait() (int, error)
	// Signal asks the child to terminate, gracefully when the platform allows.
	Signal(force bool) error
	// PID is the child process identifier, or 0 when unknown.
	PID() int
}

// LaunchSpec describes a process to run inside a pseudo-terminal.
type LaunchSpec struct {
	Command string
	Args    []string
	Workdir string
	Env     []string
	Columns uint16
	Rows    uint16
	// InheritAll passes the whole environment of the process that starts the
	// terminal to the child (then Env), instead of only the allowlist below.
	// A personal forge runs the user's own shells and agents, which need the
	// user's keys and settings; a server that runs other identities leaves it
	// false.
	InheritAll bool
	// Unset names variables removed from an inherited environment.
	Unset []string
}

// environment is the child's environment for this spec.
func (spec LaunchSpec) environment() []string {
	if spec.InheritAll {
		return inheritAllEnvironment(spec.Env, spec.Unset)
	}
	return launchEnvironment(spec.Env)
}

// inheritAllEnvironment is the whole environment, TERM defaulted, then extra.
func inheritAllEnvironment(extra, unset []string) []string {
	drop := map[string]bool{}
	for _, k := range unset {
		drop[k] = true
	}
	var result []string
	term := false
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if drop[key] {
			continue
		}
		term = term || strings.EqualFold(key, "TERM")
		result = append(result, value)
	}
	if !term {
		result = append(result, "TERM="+defaultTerm)
	}
	return append(result, extra...)
}

func (spec LaunchSpec) size() (uint16, uint16) {
	columns, rows := spec.Columns, spec.Rows
	if columns == 0 {
		columns = 120
	}
	if rows == 0 {
		rows = 32
	}
	return columns, rows
}

// LookupCommand reports whether a launch command is present on this machine, so
// a profile can be shown as unavailable instead of failing at start.
func LookupCommand(command string) (string, bool) {
	if command == "" {
		return "", false
	}
	path, err := exec.LookPath(command)
	if err != nil {
		return "", false
	}
	return path, true
}

// inherited are the variables a terminal takes, by default, from the host process's
// environment: what a shell or an agent CLI needs to run as the user (HOME
// stays, so a CLI finds the user's own login), and nothing else. Secrets
// the server was started with (API keys, GH_TOKEN, cloud credentials) and
// its own OUTCOME_* identity never reach the child; the spec's Env (OUTCOME_*
// and the profile's env, §8a.4) is added after them.
var inherited = map[string]bool{"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "SHELL": true, "TERM": true, "LANG": true, "TMPDIR": true, "TZ": true, "COLORTERM": true}

// inheritedWindows are what a Windows program needs besides those (without
// SystemRoot most fail to start); Windows keys match case-insensitively,
// as its environment does (Path is PATH).
var inheritedWindows = map[string]bool{"SYSTEMROOT": true, "WINDIR": true, "COMSPEC": true, "PATHEXT": true, "USERPROFILE": true, "APPDATA": true, "LOCALAPPDATA": true, "TEMP": true, "TMP": true, "USERNAME": true}

// allowed reports whether the server's variable key is inherited on goos.
func allowed(key, goos string) bool {
	if goos == "windows" {
		key = strings.ToUpper(key)
		if inheritedWindows[key] {
			return true
		}
	}
	return inherited[key] || strings.HasPrefix(key, "LC_")
}

// defaultTerm is TERM when the server has none.
const defaultTerm = "xterm-256color"

// launchEnvironment is the allowed part of the server's environment, TERM
// defaulted, then extra (whose entries win, being later).
func launchEnvironment(extra []string) []string {
	var result []string
	term := false
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if !allowed(key, runtime.GOOS) {
			continue
		}
		term = term || strings.EqualFold(key, "TERM")
		result = append(result, value)
	}
	if !term {
		result = append(result, "TERM="+defaultTerm)
	}
	return append(result, extra...)
}
