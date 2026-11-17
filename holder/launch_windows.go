// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package holder

import "os/exec"

// Windows panes stay in the forge's own process for now (ConPTY handles
// cannot be passed on yet): they end when the forge ends.
const supported = false

func detach(c *exec.Cmd) {}

func ignoreHangup() {}
