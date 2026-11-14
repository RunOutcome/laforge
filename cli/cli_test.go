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

func TestAReleaseRunsTheImageOfItsOwnVersion(t *testing.T) {
	defer func(v string) { Version = v }(Version)
	for v, want := range map[string]string{
		"dev":                     LocalImage,
		"v0.1.0":                  PublishedImage + ":0.1.0",
		"0.1.0":                   PublishedImage + ":0.1.0",
		"0.1.1-next+abc1234":      LocalImage,
		"9e7efa4":                 LocalImage,
		"v0.1.0-3-g9e7efa4-dirty": LocalImage,
		"v0.2.0-rc1":              PublishedImage + ":0.2.0-rc1",
	} {
		Version = v
		if got := DefaultImage(); got != want {
			t.Errorf("Version %q: image %q, want %q", v, got, want)
		}
	}
}
