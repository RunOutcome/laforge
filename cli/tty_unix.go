// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

//go:build !windows

package cli

import (
	"os"

	"golang.org/x/sys/unix"
)

// ttyFile reports whether f is a terminal (a character device such as
// /dev/null is not).
func ttyFile(f *os.File) bool {
	_, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
	return err == nil
}
