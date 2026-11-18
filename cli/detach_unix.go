// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

//go:build !windows

package cli

import (
	"os"
	"os/exec"
	"syscall"
)

// detach makes the child its own session leader, so it outlives the
// terminal that ran `la up`.
func detach(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

func terminate(p *os.Process) error { return p.Signal(syscall.SIGTERM) }

// processAlive reports whether pid is a live process.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// respawn replaces this process with argv (a forge restarting itself): the
// same pid, so a container's or a terminal's forge stays theirs.
func respawn(exe string, argv, env []string) error { return syscall.Exec(exe, argv, env) }
