// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package forge

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// The skin kit: a starting skin (la skin new) and a check that goes beyond
// what install refuses (la skin check): the manifest and files as install
// checks them, then advice: both modes styled, readable terminal colours.

// NewSkin writes a starting skin named name into dir (which must not exist
// or be empty): a two-mode manifest, a stylesheet setting the main tokens
// for light and dark, and a README.
func NewSkin(dir, name, label string) error {
	if !skinName.MatchString(name) {
		return fmt.Errorf("name %q: lower-case letters, digits and dashes, at most 40", name)
	}
	if label == "" {
		label = titleCase(name)
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s is not empty", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	r := strings.NewReplacer("{{name}}", name, "{{label}}", label)
	for file, body := range map[string]string{"skin.json": starterManifest, "skin.css": starterCSS, "README.md": starterReadme} {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(r.Replace(body)), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func titleCase(name string) string {
	words := strings.Split(name, "-")
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// SkinCheck is what checking a skin found.
type SkinCheck struct {
	Skin     Skin
	Warnings []string
}

// CheckSkin checks a skin folder as install does (an error is a refusal),
// then warns about what install allows but people will notice: a mode the
// stylesheets never style, a mode with no terminal palette, terminal text
// too faint to read.
func CheckSkin(dir string) (SkinCheck, error) {
	sk, err := ReadSkin(os.DirFS(dir))
	if err != nil {
		return SkinCheck{}, err
	}
	out := SkinCheck{Skin: sk}
	var css strings.Builder
	for _, c := range sk.CSS {
		raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(c)))
		if err != nil {
			return SkinCheck{}, err
		}
		css.Write(raw)
	}
	all := css.String()
	if len(sk.CSS) == 0 {
		out.Warnings = append(out.Warnings, "no stylesheet: the skin only changes the terminal colours")
	}
	for _, mode := range []string{"dark", "light"} {
		if len(sk.CSS) > 0 && !regexp.MustCompile(`data-mode=['"]?`+mode).MatchString(all) {
			out.Warnings = append(out.Warnings, fmt.Sprintf("no [data-mode='%s'] rules: in %s mode the skin keeps the client's own colours", mode, mode))
		}
		pal := sk.Terminal.Dark
		if mode == "light" {
			pal = sk.Terminal.Light
		}
		if len(pal) == 0 {
			out.Warnings = append(out.Warnings, fmt.Sprintf("no terminal.%s palette: the terminal keeps the client's %s colours", mode, mode))
			continue
		}
		bg, okb := parseColour(pal["background"])
		fg, okf := parseColour(pal["foreground"])
		if okb && okf {
			if ratio := contrast(fg, bg); ratio < 4.5 {
				out.Warnings = append(out.Warnings, fmt.Sprintf("terminal.%s: foreground %s on %s is %.1f:1; text wants 4.5:1", mode, pal["foreground"], pal["background"], ratio))
			}
		}
	}
	if sk.License == "" {
		out.Warnings = append(out.Warnings, "no license in skin.json: say how others may use it (Apache-2.0, CC-BY-4.0 …)")
	}
	if sk.Description == "" {
		out.Warnings = append(out.Warnings, "no description in skin.json: the theme panel and collections show it")
	}
	return out, nil
}

// parseColour reads #rgb, #rrggbb (#rrggbbaa: the alpha is ignored) and
// rgb()/rgba() into 0..1 channels.
func parseColour(s string) ([3]float64, bool) {
	s = strings.TrimSpace(s)
	var c [3]float64
	if strings.HasPrefix(s, "#") {
		h := s[1:]
		if len(h) == 3 || len(h) == 4 {
			h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
		}
		if len(h) < 6 {
			return c, false
		}
		for i := 0; i < 3; i++ {
			v, err := strconv.ParseUint(h[2*i:2*i+2], 16, 8)
			if err != nil {
				return c, false
			}
			c[i] = float64(v) / 255
		}
		return c, true
	}
	m := regexp.MustCompile(`^rgba?\(\s*([\d.]+)%?\s*,\s*([\d.]+)%?\s*,\s*([\d.]+)%?`).FindStringSubmatch(s)
	if m == nil {
		return c, false
	}
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseFloat(m[i+1], 64)
		if err != nil {
			return c, false
		}
		c[i] = math.Min(v/255, 1)
	}
	return c, true
}

// contrast is the WCAG contrast ratio of two colours.
func contrast(a, b [3]float64) float64 {
	lum := func(c [3]float64) float64 {
		var l [3]float64
		for i, v := range c {
			if v <= 0.03928 {
				l[i] = v / 12.92
			} else {
				l[i] = math.Pow((v+0.055)/1.055, 2.4)
			}
		}
		return 0.2126*l[0] + 0.7152*l[1] + 0.0722*l[2]
	}
	la, lb := lum(a), lum(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

const starterManifest = `{
  "name": "{{name}}",
  "label": "{{label}}",
  "description": "Describe the look in one line.",
  "version": "1",
  "author": "",
  "license": "Apache-2.0",
  "layout": "classic",
  "css": ["skin.css"],
  "terminal": {
    "dark": {
      "background": "#14161a", "foreground": "#e4e6ea", "cursor": "#7cc4ff",
      "cursorAccent": "#14161a", "selectionBackground": "rgba(124, 196, 255, 0.25)",
      "black": "#1e2126", "red": "#f07178", "green": "#a6d189", "yellow": "#e5c07b",
      "blue": "#7cc4ff", "magenta": "#c5a3e8", "cyan": "#7fd6d1", "white": "#cfd3da",
      "brightBlack": "#5c6370", "brightRed": "#ff8f95", "brightGreen": "#bfe3a6",
      "brightYellow": "#f0d39a", "brightBlue": "#a3d6ff", "brightMagenta": "#d8bff0",
      "brightCyan": "#a0e6e1", "brightWhite": "#f5f6f8"
    },
    "light": {
      "background": "#fbfbfc", "foreground": "#1f2329", "cursor": "#1d6fc4",
      "cursorAccent": "#fbfbfc", "selectionBackground": "rgba(29, 111, 196, 0.18)",
      "black": "#1f2329", "red": "#c4323a", "green": "#3d7a2a", "yellow": "#8a6413",
      "blue": "#1d6fc4", "magenta": "#7d4aa8", "cyan": "#18797a", "white": "#d5d9df",
      "brightBlack": "#69717d", "brightRed": "#d9474f", "brightGreen": "#4f9339",
      "brightYellow": "#a77a1b", "brightBlue": "#2f84dc", "brightMagenta": "#955fc2",
      "brightCyan": "#1f9293", "brightWhite": "#ffffff"
    }
  }
}
`

const starterCSS = `/* {{label}}: a LaForge skin. Change the colours below, then
   la skin check . && la skin install .   and reload the page.
   Every token, hook and layout: docs/skins.md in LaForge. */

/* Shapes and faces: the same in both modes. */
:root[data-skin='{{name}}'] {
  --radius-pane: 10px;
  --radius-control: 7px;
  /* --font-ui: 'Your Face', system-ui, sans-serif;  (bundle the font) */
}

/* Dark mode. */
:root[data-skin='{{name}}'][data-mode='dark'] {
  --canvas: #0f1114;    /* the page behind everything */
  --side: #14171b;      /* top bar, footer, rail */
  --card: #1a1e23;      /* pane headers, panels, menus */
  --term: #14161a;      /* behind the terminal: match terminal.dark.background */
  --line: #2a3038;      /* borders */
  --line-2: #20252b;
  --tx: #e4e6ea;        /* text */
  --tx-2: #b4bac3;
  --tx-3: #7d8591;
  --acc: #7cc4ff;       /* the accent */
  --acc-fill: #3a8fd9;
  --on-acc: #0b0d10;
  --red: #f07178;       /* blocked, error */
  --amber: #e5c07b;     /* working */
  --green: #a6d189;     /* running */
}

/* Light mode. */
:root[data-skin='{{name}}'][data-mode='light'] {
  --canvas: #f3f4f6;
  --side: #eceef1;
  --card: #ffffff;
  --term: #fbfbfc;      /* match terminal.light.background */
  --line: #d5d9df;
  --line-2: #e3e6ea;
  --tx: #1f2329;
  --tx-2: #4b525c;
  --tx-3: #737b86;
  --acc: #1d6fc4;
  --acc-fill: #1d6fc4;
  --on-acc: #ffffff;
  --red: #c4323a;
  --amber: #8a6413;
  --green: #3d7a2a;
}

/* Deeper: style by hook, e.g. a pane that needs you.
[data-skin='{{name}}'] .pane[data-status='blocked'] { box-shadow: 0 0 0 2px var(--red); }
*/
`

const starterReadme = `# {{label}}

A [LaForge](https://github.com/RunOutcome/laforge) skin, in light and dark.

    la skin install ./{{name}}     # or owner/repo once it is on GitHub
    la skin use {{name}}

Made with ` + "`la skin new`" + `. How skins work: docs/skins.md in LaForge.
`
