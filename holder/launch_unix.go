// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

//go:build !windows

package holder

import (
	"os/exec"
	"os/signal"
	"syscall"
)

const supported = true

// detach makes the holder its own session leader: it outlives the forge
// and the terminal that started the forge.
func detach(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

func ignoreHangup() { signal.Ignore(syscall.SIGHUP, syscall.SIGINT, syscall.SIGPIPE) }
