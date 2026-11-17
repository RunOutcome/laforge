<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- SPDX-FileCopyrightText: 2026 COR Holdings, LLC -->

# Skins: a guide for skin authors

A skin changes how the forge looks: colours, faces, shapes, and where
things sit on the screen. It is a folder holding a `skin.json` manifest
and the files that manifest names: stylesheets, fonts and images.

**A skin holds no code.** The forge accepts `.css`, `.json`, `.woff2`,
`.woff`, `.ttf`, `.otf`, `.png`, `.jpg`, `.jpeg`, `.webp`, `.gif`, `.svg`,
`.md` and `.txt` files and nothing else. It refuses a folder with any other
file type, with links in it, or larger than 16 MB. It serves each file with
its own content type, `nosniff` and a sandboxing policy, so even an SVG
cannot run script. Anything a skin draws, it draws with CSS.

LaForge ships two skins: **classic** (its own look, light and dark) and
**console** (a black control-room look in the rail layout). A skin you
install is used in the browser that picks it. The forge's default skin is
what other browsers see until they pick their own.

**Every skin draws light and dark.** The viewer chooses the mode; the
client sets `data-mode="light"` or `data-mode="dark"` on the root and the
skin styles both.

## Install and choose

```
la skin install ./my-skin        # check the folder and copy it into the forge's home
la skin install you/theme-pack   # every skin in a GitHub repository
la skin list                     # the skins the forge has, and its default
la skin use my-skin              # make it the forge's default
la skin update [my-skin]         # reinstall one skin, or all, from where it came
la skin remove my-skin           # remove an installed skin (built-ins stay)
```

Installed skins live in `<home>/skins/<name>/` (`~/.laforge/forge/skins` by
default, or under `$LAFORGE_HOME`). Installing a skin with the name of one
already installed replaces it. A built-in name cannot be taken.

In the browser, ⌘K (or Ctrl-b `:`) lists **Skin: <label>** for every skin.
Picking one switches at once, without a reload, and is remembered in that
browser. **Make <label> the forge default** sets the forge's default. A skin
whose stylesheet fails to load falls back to classic.

## The manifest: skin.json

```json
{
  "name": "harbour",
  "label": "Harbour",
  "description": "Slate and brass, condensed headings.",
  "version": "1",
  "author": "You",
  "license": "Apache-2.0",
  "layout": "classic",
  "css": ["skin.css"],
  "terminal": {
    "dark": { "background": "#0d1418", "foreground": "#e3e9ec", "cursor": "#d4a04c" },
    "light": { "background": "#f4f1ea", "foreground": "#1d262b", "cursor": "#9a6a1c" }
  }
}
```

| Field | Meaning |
| --- | --- |
| `name` | The skin's id: lower-case letters, digits and dashes, at most 40. It must match the folder's name once installed. |
| `label` | What people see in the palette. Defaults to `name`. |
| `description`, `version`, `author`, `license` | Shown in `la skin list`; say what licence your files are under. |
| `layout` | `classic` (tabs above panes) or `rail` (spaces and crew in a rail at the left). Defaults to `classic`. |
| `css` | The stylesheets, in order, relative to the folder. Optional: a skin may change only the terminal palette. |
| `terminal` | The terminal's colours, one palette per mode: `{"dark": {…}, "light": {…}}`. Either may be left out, and that mode keeps the client's own palette. Keys: `background`, `foreground`, `cursor`, `cursorAccent`, `selectionBackground`, `selectionForeground`, `black`, `red`, `green`, `yellow`, `blue`, `magenta`, `cyan`, `white` and `brightBlack` … `brightWhite`. Values are `#hex` or `rgb()`/`rgba()`. Keys left out keep the mode's own colours. |

Unknown fields are refused, so a typo is caught when you install. An old
`mode` field is refused too: skins draw both light and dark, so remove it
and style both modes.

## How a skin is applied

The client adds one `<link rel="stylesheet">` per `css` entry, from
`/skins/<name>/<file>`, and sets two attributes on the root element:

```html
<html data-skin="harbour" data-layout="classic" data-mode="dark">
```

Scope every rule to your skin, so nothing leaks while a switch is under way:

```css
:root[data-skin='harbour'] { --acc: #d4a04c; }
[data-skin='harbour'] .pane-title { letter-spacing: 0.04em; }
```

Fonts and images use relative `url()`s, which resolve inside the skin's
folder. Bundle fonts as `woff2` with their licence; the forge works offline,
so do not load anything from the network.

```css
@font-face {
  font-family: 'Harbour Display';
  src: url('fonts/display-600.woff2') format('woff2');
  font-weight: 600;
}
```

## Custom properties

Most of a skin is setting these on `:root[data-skin='<name>']`. Classic sets
them on `:root` (light) and `:root[data-mode='dark']`.

**Colours**

| Property | Used for |
| --- | --- |
| `--canvas` | the page behind everything |
| `--side` | the top bar, footer, rail and chrome |
| `--sunken` | recessed areas |
| `--card` | panels, pane headers, menus, inputs |
| `--term` | the pane body behind the terminal |
| `--hover`, `--press` | hover and pressed fills |
| `--line`, `--line-2` | borders, strong and faint |
| `--tx`, `--tx-2`, `--tx-3` | text: primary, secondary, muted |
| `--acc`, `--acc-500`, `--acc-100`, `--acc-fill`, `--on-acc` | the accent: text, light, tint, solid fill, text on the fill |
| `--ai`, `--ai-600`, `--ai-100`, `--ai-50` | agent chips and labels |
| `--red`, `--red-50` | blocked, error, failed |
| `--amber`, `--amber-50` | working, warnings |
| `--green` | running |
| `--todo` | idle, ended |
| `--on-red` | text on a red count bubble |
| `--scrim` | behind dialogs |
| `--e2`, `--e3` | shadows of raised and floating things |
| `--shadow-tab` | the active tab's shadow |

**Faces**

| Property | Used for |
| --- | --- |
| `--font-ui` | all interface text |
| `--font-mono` | paths, keys, counts, messages |
| `--font-display` | big titles (the rail's machine and space names); defaults to `--font-ui` |

The terminal keeps its own monospace face.

**Shapes and sizes**

| Property | Classic | Used for |
| --- | --- | --- |
| `--radius-pane` | 10px | pane frames, the agents panel |
| `--radius-dialog` | 12px | the palette and help |
| `--radius-card` | 9px | profile cards, the ended bar |
| `--radius-mono` | 8px | profile monograms |
| `--radius-control` | 7px | tabs, icon buttons, rows |
| `--radius-button` | 6px | text buttons |
| `--radius-button-sm` | 5px | small buttons |
| `--radius-chip` | 4px | badges, chips |
| `--radius-pill` | 999px | counts |
| `--topbar-height` | 40px | the tab bar |
| `--statusbar-height` | 26px | the footer |
| `--pane-head-height` | 30px | a pane's header |
| `--work-gap` | 6px | space around the panes |
| `--pane-gap` | 3px | space between panes |
| `--rail-width` | 248px | the rail (rail layout) |

## Hooks

Every region has a class, and the ones that change with state carry data
attributes. These are stable: a skin may rely on them.

**Root and layout**

| Hook | What |
| --- | --- |
| `html[data-skin]`, `html[data-layout]`, `html[data-mode]` | the skin, `classic` or `rail`, `light` or `dark` |
| `.forge[data-layout]` | the whole client; `.layout-rail` in the rail layout, `.is-rail-open` while the phone drawer is open |
| `.forge.is-embedded` | mounted inside a host page |
| `.forge-main` (classic, adds no box), `.rail-main` (rail) | the part right of the rail |

**Tab bar**

| Hook | What |
| --- | --- |
| `.topbar` | the tab bar (`.topbar.rail-tabs` in the rail layout) |
| `.brand`, `.brand-name` | the LaForge mark (classic) |
| `.tabs` | the tab list |
| `.tab[data-status]`, `.tab.is-active` | a tab; `data-status` is its most urgent pane: `blocked`, `error`, `done`, `working`, `idle`, or, with no reports, `running`, `failed`, `complete`, `empty` |
| `.tab-name`, `.tab-count`, `.tab-actions`, `.tab-rename`, `.tab-confirm` | parts of a tab |
| `.new-pane-btn`, `.menu`, `.menu-item` | the New pane button and its menu |
| `.tools`, `.icon-btn`, `.agents-btn`, `.cmd-btn` | the buttons at the right |

**Panes**

| Hook | What |
| --- | --- |
| `.tab-bodies`, `.tab-body.is-active` | the panes' area, one per tab |
| `.pane-slot` | where a pane sits |
| `.pane[data-status][data-active]` | a pane frame; `data-status` is its shown status (`blocked`, `error`, `done`, `working`, `idle`), else its process state (`starting`, `running`, `idle`, `complete`, `failed`), `lost` or `empty`; `data-active` is `true` for the focused pane of the active tab |
| `.pane.is-focused`, `.pane.is-zoomed` | the same, as classes |
| `.pane-head`, `.pane-title`, `.pane-msg`, `.pane-cwd`, `.pane-tools`, `.pane-held`, `.pane-lost`, `.chip`, `.chip-agent` | the header and its parts |
| `.badge.badge-<status>` | the status badge |
| `.ask`, `.ask-allow`, `.ask-deny`, `.ask-note` | Allow and Deny |
| `.pane-body`, `.term-host` | the terminal's box |
| `.picker`, `.profile`, `.lost-pane`, `.lost-actions`, `.ended` | an empty, lost or ended pane |
| `.gutter.gutter-row`, `.gutter.gutter-col` | the gaps you drag |
| `.dot.dot-<state>`, `.dot.st-<status>` | status dots |

**Panels, palette, banners**

| Hook | What |
| --- | --- |
| `.agents`, `.agents-head`, `.agents-list`, `.agents-foot` | the agents panel |
| `.agent-row[data-status]`, `.agent-row.is-current`, `.agent-row.is-child`, `.label-chip`, `.agents-lost`, `.lost-row` | its rows |
| `.palette`, `.palette li.is-active`, `.pal-group`, `.pal-label` | the ⌘K palette |
| `.scrim`, `.help` | dialogs and their backdrop |
| `.view-banner` | the read-only share banner |
| `.statusbar`, `.sb-*` | the classic footer |

**Rail layout**

| Hook | What |
| --- | --- |
| `.rail` | the rail at the left (a drawer below 640px) |
| `.rail-machine`, `.rail-label`, `.rail-host` | the machine's name |
| `.rail-section.rail-spaces`, `.rail-title`, `.rail-count` | the Spaces section |
| `.rail-space[data-space][data-index]`, `.rail-space.is-active` | a space; `data-space` is its name (`""` for All), `data-index` its position modulo 8, for colour blocks |
| `.rail-add`, `.rail-new-space` | New space and its form |
| `.rail-section.rail-crew`, `.crew-row[data-status]`, `.crew-row.is-current` | the Crew and its rows |
| `.crew-bar`, `.crew-name`, `.crew-sub`, `.crew-msg` | a crew row's parts |
| `.rail-header`, `.rail-when`, `.rail-date`, `.rail-time`, `.rail-space-title`, `.rail-toggle` | the header |
| `.rail-footer`, `.rf-counts`, `.rf-count[data-status]` | the footer and its counts (`working`, `blocked` for need you, `done`) |
| `.rail-scrim` | behind the open drawer on a phone |

## The two layouts

**classic**: the tab bar at the top, the panes, a footer with the tabs and
counts. Light and dark.

**rail**: a rail at the left with the machine, the **Spaces** and the
**Crew**; at the right a header (date, time and the space, large), the tabs,
the panes and a footer (`N working · N need you · N done`, the keys hint and
the clock).

- A pane's *space* is its `space` label (`la run --label space=billing …`, or
  **+ New space**), else the last part of its folder. Clicking a space shows
  only the tabs that hold its panes; **All** shows every tab.
- The *crew* is every pane that reports a status or runs an agent profile,
  most urgent first, with Allow and Deny inline.
- Below 640px the rail becomes a drawer, opened with `.rail-toggle`.

The client draws the rail's structure plainly; a rail skin gives it its
look. `forge/skins/console/skin.css` is a full example: elbow bars that join
the rail to the header and footer (a thick `border-top` on `.rail-header`, a
coloured `.rail-machine`, and a `radial-gradient` corner on
`.rail-header::before`), colour blocks from `.rail-space[data-index]`, and
tabs coloured by `.tab[data-status]`.

## A minimal skin

```
harbour/
  skin.json
  skin.css
```

`skin.json`:

```json
{ "name": "harbour", "label": "Harbour", "license": "Apache-2.0", "css": ["skin.css"] }
```

`skin.css`:

```css
:root[data-skin='harbour'] {
  --side: #f4f1ea;
  --canvas: #f4f1ea;
  --card: #ffffff;
  --term: #f4f1ea;
  --line: #d8d1c2;
  --acc: #9a6a1c;
  --acc-fill: #b9852f;
  --radius-pane: 4px;
}
:root[data-skin='harbour'][data-mode='dark'] {
  --side: #0d1418;
  --canvas: #0d1418;
  --card: #132026;
  --term: #0d1418;
  --line: #24363e;
  --acc: #d4a04c;
}
[data-skin='harbour'] .tab.is-active { box-shadow: inset 0 -2px 0 var(--acc); }
```

Then `la skin install ./harbour` and pick **Skin: Harbour** in ⌘K.

## Theme packs

`la skin install SOURCE` takes a folder or a GitHub repository, holding
one skin or many, and installs every skin it finds. A skin that fails its
checks is reported and the others still install.

**Sources**

| Form | Example |
| --- | --- |
| a folder | `./harbour`, `~/skins/pack`, `/srv/skins` |
| a repository | `you/skins`, `github:you/skins` |
| at a branch, tag or commit | `you/skins@v2` |
| a folder in it | `you/skins//themes/harbour`, `you/skins@v2//themes` |
| a link | `https://github.com/you/skins`, `https://github.com/you/skins/tree/v2/themes` |

Only public repositories on github.com: the forge downloads
`codeload.github.com/<owner>/<repo>/tar.gz/<ref>` (the default branch when
no ref is given), at most 32 MB, and unpacks only files and folders (an
archive with links, devices, absolute paths or `..` is refused). Nothing it
downloads runs; each skin passes the same checks as a folder.

A folder installs straight into the forge's home, with or without a forge
running. A GitHub source is fetched by the running forge, or by `la`
itself when none runs.

**Layout of a source**

One skin: `skin.json` at the root.

```
harbour/
  skin.json
  skin.css
```

Many skins: one per folder, at the root or under `skins/`.

```
my-skins/                 my-skins/
  harbour/                  README.md
    skin.json               skins/
    skin.css                  harbour/skin.json …
  ember/                      ember/skin.json …
    skin.json
```

Each installed skin remembers where it came from in
`<home>/skins/<name>/.source.json` (`{source, ref, installedAt}`), shown in
`la skin list`. `la skin update NAME` reinstalls it from there, at the same
ref; `la skin update` does so for every skin with a source.

## Rules

- No code: stylesheets, fonts and images only.
- Nothing from the network: bundle what you use.
- Fonts need a licence that lets you bundle them (SIL OFL, for instance); put
  the licence text in the skin.
- Scope rules to `[data-skin='<name>']`.
- Draw only what the hooks give you; do not depend on other class names,
  which may change.
