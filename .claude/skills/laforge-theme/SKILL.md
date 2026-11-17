---
name: laforge-theme
description: Build, restyle or fix a LaForge theme (a "skin"): scaffold it with la skin new, design light and dark modes, style it through LaForge's custom properties and hooks, check it with la skin check, install and screenshot it, and package one or many for sharing as a collection. Use when someone asks for a new LaForge theme or skin, wants one to look like something, or has a skin that will not install.
---

# Building a LaForge theme

A LaForge theme (the code calls it a skin) is a folder: `skin.json`, one or
more stylesheets, and any fonts and images they use. **Never code**: no
JavaScript, HTML or anything else that runs; install refuses it. Every theme
draws **both light and dark**.

The full reference is `docs/skins.md` in the LaForge repository (every
custom property, every hook, both layouts, packs, collections). Read it, and
read one finished skin before designing: `forge/skins/console/` (built in,
rail layout) or any folder in `skins/` (the official ones). Outside the
repository: https://github.com/RunOutcome/laforge/blob/main/docs/skins.md

## Steps

1. **Agree the look first.** Ask, or infer from the request: the mood, two to
   four colours, layout (`classic`: tabs above panes; `rail`: a left rail of
   spaces and agents with a status footer), and a font if any. Write it in
   one line; it becomes `description`.

2. **Scaffold:** `la skin new <name> [dir]`. The name is lower-case letters,
   digits and dashes; `dir` is the skin folder itself (default `./<name>`).
   It writes a manifest with both terminal palettes, a stylesheet with the
   main tokens for both modes, and a README. Fill in `author` (the person
   you build it for, or ask) and replace the placeholder `description`.

3. **Design dark and light together**, in `skin.css`:
   - colours under `:root[data-skin='<name>'][data-mode='dark']` and
     `…[data-mode='light']`; shapes and fonts under `:root[data-skin='<name>']`;
   - start with the tokens that change the most: `--canvas`, `--side`,
     `--card`, `--term`, `--line`, `--tx`/`--tx-2`/`--tx-3`, `--acc`,
     `--acc-fill`, `--on-acc`, then the status colours `--red` (blocked,
     error), `--amber` (working), `--green` (running); **done** is shown in
     the accent, `--acc`;
   - keep `--term` equal to that mode's `terminal.<mode>.background` in
     `skin.json`, or the pane shows a seam;
   - the light mode is a design of its own, not the dark one inverted: the
     same identity (accents, shapes, type) on a light ground.

4. **Go deeper with hooks** only when tokens are not enough:
   `.tab[data-status]`, `.pane[data-status][data-active]`,
   `.pane-head`, the agents panel, and in the rail layout `.rail`, the
   spaces and crew rows, `.rail-when`, the footer counts. Scope every rule
   with `[data-skin='<name>']` so it never leaks into other skins. A pane's
   status colours its border; make the focused pane show on top of that
   with a ring, e.g.
   `[data-skin='<name>'] .pane[data-active='true'] { box-shadow: 0 0 0 2px var(--acc); }`,
   so the active pane still stands out when it is also working. Use the
   hook names from `docs/skins.md`; do not target generated or unlisted
   class names, which may change.

5. **The terminal palette** (`terminal.dark`, `terminal.light` in
   `skin.json`): the xterm keys (`background`, `foreground`, `cursor`,
   `cursorAccent`, `selectionBackground`, `black` … `brightWhite`), values
   `#hex` or `rgb()`/`rgba()`. Keep the 16 ANSI colours distinguishable on
   their background in both modes; programs use them for meaning.

6. **Fonts:** only fonts under the SIL OFL or Apache-2.0, bundled in the
   skin as woff2 (`fonts/…`) with their licence file next to them, loaded
   with `@font-face` and a relative `url()`. No CDN: LaForge works offline.
   The terminal keeps its own monospace face.

7. **Check, then install:**
   ```sh
   la skin check --strict <dir>   # refusals as install gives them, then warnings
   la skin install <dir>
   la skin use <name>             # or pick it in the Themes panel
   ```
   `--strict` exits 1 on any warning (without it, warnings still exit 0).
   Fix every warning. It checks: each mode styled and with a terminal
   palette; terminal text, `--tx` on `--canvas`/`--card`/`--side` and
   `--on-acc` on `--acc-fill` at 4.5:1 or better; `--term` matching the
   terminal background; the folder named as the skin; a licence and a
   description. Values it cannot read as colours (`var()`, gradients) it
   does not judge: look at those yourself. After each edit run
   `la skin install <dir>` again and reload the page (skin files are served
   uncached).

8. **Look at it, in both modes,** in a scratch forge so nobody's work is
   touched. Export the home first, so **every** later `la` command uses it
   (setting it on `la up` alone sends the rest to the user's real forge):
   ```sh
   export LAFORGE_HOME=$(mktemp -d)
   la up --no-open --port 47400         # prints the URL; the page needs its ?token=
   la status                            # the port it really got
   la skin install <dir> && la skin use <name>
   la run --bg --name tests  sh -c 'la report working --msg "running the suite"; exec sh'
   la run --bg --name review sh -c 'la report blocked --kind question --msg "Which branch?"; exec sh'
   la run --bg --name docs   sh -c 'la report done --msg "README updated"; exec sh'
   ```
   Open the printed URL. Panes started from the command line open in a tab
   of their own (named "Found"); click it. The mode is the
   `laforge.mode` localStorage key (`light`, `dark` or `system`): set it and
   reload, or use the header's light/dark button. For headless screenshots,
   drive Chrome over the DevTools protocol: set the window size (1400x875,
   and 390x844 for a phone), set the key, reload, click the tab with real
   mouse events (a script's `.click()` is not enough), and capture.
   Check: text readable, blocked, working and done tell apart, the active
   pane stands out, nothing a skin draws overflows at 390px. Then
   `la down` (it stops only this forge) and delete the home.

9. **Share it.** A GitHub repository holding the skin at its root, or many
   skins in subfolders (or under `skins/`), installs with
   `la skin install owner/repo`. Add a `collection.json` at the root
   (`{"id", "name", "description", "skins": [{"name", "label",
   "description", "path"}]}`) and people can add it in the Themes panel by
   link. For the LaForge library (RunOutcome/laforge-skins), open a pull
   request; its rules are below.

## Rules

- Stylesheets, fonts and images only (`.css .woff2 .woff .ttf .otf .png
  .jpg .jpeg .webp .gif .svg .json .md .txt`), 16 MB at most, no links.
- `skin.json` has no `mode` key (that was the old one-mode format). Its
  `name` is what the skin installs as; keep its folder named the same.
- Original work for anything public or in the library: no trademarks,
  logos, characters or a franchise's recognisable look. A personal fan skin
  belongs in a private folder, not in a shared collection.
- Give it a `license` and an `author` in `skin.json`.

## When a skin will not install

The message names the problem; the common ones:

| Message | Fix |
| --- | --- |
| `skin.json: skins draw both light and dark; remove mode` | delete `"mode"`, split `terminal` into `dark` and `light`, add `[data-mode]` rules |
| `a skin carries stylesheets, fonts and images only` | remove the named file (scripts, HTML, binaries) |
| `css "…" is not in the skin` | fix the path in `css`, relative to the skin folder |
| `terminal.<mode>.<key> "…": a known key and a #hex or rgb() colour` | use an xterm key and a plain colour (no names, no `var()`) |
| `name "…": lower-case letters, digits and dashes, at most 40` | rename it, and its folder |
| `… is a built-in skin; give yours another name` | `classic` and `console` are taken |
| `unknown field "…"` | remove keys `skin.json` does not define |
