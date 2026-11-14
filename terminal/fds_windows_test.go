// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

//go:build windows

package terminal

// openDescriptors is 0 on Windows, where the test skips (posix).
func openDescriptors() int { return 0 }
