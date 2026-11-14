// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package forge

import "embed"

// embedded is the built web client (web/ -> forge/dist, `make web`). The
// built files are committed so `go get` and `go install` work without Node.
//
//go:embed all:dist
var embedded embed.FS

// Notices is THIRD_PARTY_NOTICES.md as built into the web client.
func Notices() string {
	raw, err := embedded.ReadFile("dist/notices.txt")
	if err != nil {
		return "Third-party notices are not in this build; see THIRD_PARTY_NOTICES.md in the source.\n"
	}
	return string(raw)
}
