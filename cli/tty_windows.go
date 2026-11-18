// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

//go:build windows

package cli

import (
	"os"

	"golang.org/x/sys/windows"
)

// ttyFile reports whether f is a console.
func ttyFile(f *os.File) bool {
	var mode uint32
	return windows.GetConsoleMode(windows.Handle(f.Fd()), &mode) == nil
}
