// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package cli

import (
	"bytes"
	"strings"
	"testing"
)

// A host mounts the tree under its own name (Outcome: `out la`); usage and
// refusals then speak that name.
func TestAHostMountsTheTreeUnderItsOwnName(t *testing.T) {
	t.Setenv("LAFORGE_HOME", t.TempDir())
	var out, errb bytes.Buffer
	cfg := Config{Prog: "out la", Reexec: []string{"la"}, Stdout: &out, Stderr: &errb}
	if code := Main(cfg, []string{"help"}); code != 0 {
		t.Fatalf("help: %d", code)
	}
	if !strings.Contains(out.String(), "out la up") {
		t.Fatalf("usage does not name the host: %q", out.String())
	}
	out.Reset()
	if code := Main(cfg, []string{"status"}); code != 0 || !strings.Contains(out.String(), "out la up") {
		t.Fatalf("status when stopped: %d %q", code, out.String())
	}
	if code := Main(cfg, []string{"bogus"}); code != 2 || !strings.Contains(errb.String(), `out la: unknown command "bogus"`) {
		t.Fatalf("unknown command: %d %q", code, errb.String())
	}
}
