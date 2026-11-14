# Changelog

All notable changes to LaForge. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[Semantic Versioning](https://semver.org/). A release's notes are its section
here, so each tag needs one.

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
