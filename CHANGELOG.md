# Changelog

All notable changes to LaForge. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[Semantic Versioning](https://semver.org/). A release's notes are its section
here, so each tag needs one.

## [Unreleased]

Agents you can see, keep and hand work to.

### Added

- **Settings, in the browser and in `la config`.** One settings model behind
  `GET`/`PUT /api/config` (a partial update; refused whole on a bad key or
  value): the new-pane folder (now saved in `config.json`, so it survives a
  restart), the default skin, scenes on or off, and profiles (`PUT`/`DELETE
  /api/profiles/{name}`; a command not on PATH is kept with a warning), with
  version, host, user, port and home read-only. A **Settings** panel (the
  gear, or ⌘K) shows them with this browser's own (light/dark,
  notifications, sound, text size, now remembered), the shares with Revoke,
  and About with Restart (`POST /api/restart?respawn=1`: the forge starts
  the next one itself). `la config`, `la config get|set|unset`, `la config
  profile add|remove` use the same keys, through the running forge or
  straight into the home.
- **Animated skins (scenes).** A skin may carry a PixiJS scene that draws
  behind the panes and reacts to their status. It is off until you allow it
  (Themes panel, `la skin allow`, `la skin install --allow-code`), pinned to a
  hash of the code, and run in a sandbox with no network, API, token or
  terminal access. `la skin scenes off` turns them all off (docs/skins.md).
- **Swap panes** by dragging one by its header onto another.
- **Tall tabs.** A tab can be 1x, 2x, 3x or 4x screens tall, so many panes
  are not crunched together: the tab scrolls and its panes spread over the
  taller area, none taller than one screen. Pick the size beside New pane,
  in the palette or with `^b s`; page with the dots at the edge or
  `^b PgUp` / `^b PgDn`. The wheel scrolls over headers and gutters and stays
  the terminal's own inside one; a focused pane scrolls into view. The size is
  kept with the layout.

- **Agent status.** A program reports what it is doing with OSC 7501, the
  Program Status Protocol herdr-core reads (docs/status-protocol.md), or
  with `la report`. `la hooks install` makes Claude Code and Codex report
  through their own hooks; the hook does nothing outside a LaForge pane, and
  your settings file keeps its order (uninstall gives it back byte for byte).
- **In the browser:** status badges on panes and tabs, an agents panel
  (Ctrl-b a) with the most urgent first, browser notifications and an
  optional chime when an agent is blocked, errs or is done, and the blocked
  count in the page title. "Done" turns to idle once you have looked.
- **Panes outlive the forge.** Each pane runs in a holder process of its
  own; `la restart` (after an update) keeps every pane running on the same
  port and token, and open browsers reconnect. A forge that dies gives its
  token to the next one. Not yet on Windows.
- **Resume.** Panes whose program is gone (`la down`, a reboot) are offered
  to resume; Claude Code and Codex panes continue their own session
  (`claude --resume`, `codex resume`).
- **Agents run agents.** `la run [--bg]`, `la ls`, `la send`, `la read`,
  `la wait`, `la events`, `la close`, and `la --skill` (how an agent uses
  them, in one read). A pane started from a pane is its child: it ends with
  its parent unless `--outlive-parent`, and the parent is told when it is
  blocked, errs, is done or ends. A send waits while a person is typing.
- **Answer from the browser.** With `la hooks install claude --answer 90s`,
  Claude Code's permission requests show Allow and Deny on the pane (and
  `la answer`); with no answer in time Claude asks in its terminal as usual.
- **Share a pane read-only:** `la share PANE` gives a link that watches one
  pane, ignores its input, expires and can be revoked.
- **Themes:** installable skins, each in light and dark: Classic, Console
  and seven official ones; install from collections (official, library, or
  any repo added by link) in the Themes panel or with `la skin`.
  `la skin new` starts one and `la skin check` checks it; the
  `laforge-theme` agent skill builds one (docs/skins.md).
- `GET /api/events` (server-sent pane changes) and the API as a contract,
  docs/openapi.yaml, checked against the routes by a test.

## [0.1.0] - 2026-11-14

The first release of the new, standalone LaForge: many shells and AI agents,
side by side, in your browser.

### Added

- `la up` starts a local forge (127.0.0.1, port 4646 or the next free one)
  and opens it; `la down`, `la status`, `la open`, `la serve`, `la notices`.
- Tabs of split panes, tmux-style: Ctrl-b prefix keys, a ⌘K command palette,
  renamable tabs, a New pane menu, zoom, tiled and main + stack layouts, and
  bigger / smaller text. Panes keep running when the browser closes and
  reattach with their scrollback.
- Agent panes for Claude Code, Codex, Antigravity, Qwen Code, GitHub Copilot,
  Grok, opencode and Ollama, shown when the CLI is installed; your own in
  `~/.laforge/forge/profiles.json`. When an agent exits, its pane becomes a
  shell in the same folder.
- `la up --container` runs the forge in a container with Codex, Qwen Code and
  opencode bundled; Claude Code, Copilot and Antigravity install on first start
  into the container's home volume. Rootless Podman is supported.
- Token sign-in: the link `la up` prints sets an HttpOnly cookie; changes are
  accepted only from the forge's own page.
- Light and dark themes in Outcome's look; IBM Plex type.
- A Go library: `forge.NewServer` is an `http.Handler`, and `cli.Main` lets
  another CLI mount `la` (Outcome mounts it as `out la`).

### Known limitations

- Windows builds are experimental: they compile and run the unit tests, but
  have had little hands-on use, and agent panes do not drop to a shell when
  the agent exits.
