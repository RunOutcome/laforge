// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

//go:build windows

package cli

import (
	"os"
	"os/exec"
	"syscall"
)

const (
	createNewProcessGroup = 0x00000200
	detachedProcess       = 0x00000008
)

func detach(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup | detachedProcess}
}

// terminate ends the forge; Windows has no SIGTERM, so its panes end with it.
func terminate(p *os.Process) error { return p.Kill() }

// processAlive reports whether pid is a live process.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid) // opens a handle; fails for a gone pid
	if err != nil {
		return false
	}
	p.Release()
	return true
}

// respawn starts argv as the next forge, detached, for this one to exit.
func respawn(exe string, argv, env []string) error {
	c := exec.Command(exe, argv[1:]...)
	c.Env = env
	detach(c)
	if err := c.Start(); err != nil {
		return err
	}
	return c.Process.Release()
}
