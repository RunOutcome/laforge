// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package forge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A new skin installs as made, styles both modes, reads well, and checks
// with no warning but its placeholder description; a skin missing a mode
// or with faint text is warned about; one install refuses is refused.
func TestTheSkinKit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "harbour-lights")
	if err := NewSkin(dir, "harbour-lights", ""); err != nil {
		t.Fatal(err)
	}
	if err := NewSkin(dir, "harbour-lights", ""); err == nil {
		t.Fatal("wrote over a skin")
	}
	if err := NewSkin(t.TempDir()+"/x", "Bad Name", ""); err == nil {
		t.Fatal("bad name accepted")
	}
	res, err := CheckSkin(dir)
	if err != nil || res.Skin.Label != "Harbour Lights" || len(res.Warnings) != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := InstallSkin(t.TempDir(), dir); err != nil {
		t.Fatalf("a new skin does not install: %v", err)
	}
	css := filepath.Join(dir, "skin.css")
	raw, _ := os.ReadFile(css)
	os.WriteFile(css, []byte(strings.ReplaceAll(string(raw), "[data-mode='light']", "[data-x]")), 0o644)
	man := filepath.Join(dir, "skin.json")
	raw, _ = os.ReadFile(man)
	os.WriteFile(man, []byte(strings.Replace(string(raw), `"foreground": "#e4e6ea"`, `"foreground": "#2a2d33"`, 1)), 0o644)
	res, err = CheckSkin(dir)
	joined := strings.Join(res.Warnings, "\n")
	if err != nil || !strings.Contains(joined, "no [data-mode='light'] rules") || !strings.Contains(joined, "terminal.dark: foreground #2a2d33") {
		t.Fatalf("%v\n%s", err, joined)
	}
	// a seam, faint interface text, a folder named otherwise
	raw, _ = os.ReadFile(css)
	bad := strings.Replace(string(raw), "--term: #14161a;", "--term: #ff0000;", 1)
	bad = strings.Replace(bad, "--tx: #e4e6ea;", "--tx: #1a2e31;", 1)
	os.WriteFile(css, []byte(bad), 0o644)
	moved := filepath.Join(filepath.Dir(dir), "elsewhere")
	os.Rename(dir, moved)
	res, _ = CheckSkin(moved)
	joined = strings.Join(res.Warnings, "\n")
	for _, want := range []string{"dark mode: --term #ff0000 is not terminal.dark.background", "dark mode: --tx #1a2e31 on --canvas", "the folder is elsewhere but skin.json names harbour-lights"} {
		if !strings.Contains(joined, want) {
			t.Errorf("no warning %q in:\n%s", want, joined)
		}
	}
	dir = moved
	os.WriteFile(filepath.Join(dir, "x.js"), []byte("x"), 0o644)
	if _, err := CheckSkin(dir); err == nil {
		t.Fatal("a skin with code checked")
	}
	if r := contrast([3]float64{0, 0, 0}, [3]float64{1, 1, 1}); r < 20.9 || r > 21.1 {
		t.Fatalf("contrast black on white %.2f", r)
	}
	if c, ok := parseColour("rgba(255, 0, 0, 0.5)"); !ok || c[0] != 1 || c[1] != 0 {
		t.Fatalf("%v %v", c, ok)
	}
}
