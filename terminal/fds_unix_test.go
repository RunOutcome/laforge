// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

//go:build !windows

package terminal

import "syscall"

// openDescriptors counts the open descriptors below 4096 with fcntl.
func openDescriptors() int {
	n := 0
	for fd := uintptr(0); fd < 4096; fd++ {
		if _, _, errno := syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFD, 0); errno == 0 {
			n++
		}
	}
	return n
}
