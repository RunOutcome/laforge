# LaForge

Many shells and agents, side by side, in your browser. LaForge is tmux for
the agent era: one small local server keeps your terminals running, and a
web client lays them out as tabs and split panes. Start Claude Code, Codex,
Gemini, Copilot or a plain shell in any pane, close the browser, come back
later, and everything is where you left it.

```sh
la up        # start the forge (or find the running one) and open it
la status    # the forge and its panes
la open      # a fresh link to the running forge
la down      # stop the forge and every pane in it
```

LaForge is part of [Outcome](https://github.com/RunOutcome), and works on its own.

## Build

Needs Go 1.24+. Node 20+ only to change the web client.

```sh
make build      # bin/la (the built web client in forge/dist is committed)
make install    # copies it to ~/.local/bin/la-<version>; la points at it
make test       # Go and web tests
make web        # rebuild the web client after changing web/
make dev        # web client with hot reload against a running `la up`
```

## Using it

`la up` starts the forge in the background on `127.0.0.1:4646` (any free
port if that one is taken), prints a link and opens it. The link carries a
token; the browser keeps it as a cookie, and nothing else on the machine
can reach the panes without it. New panes start in the folder you ran
`la up` from.

Keys follow tmux: press the prefix **Ctrl-b**, then

| Key | Does |
| --- | --- |
| `%` | split right |
| `"` | split down |
| arrows or `h j k l` | move between panes |
| `o` | next pane |
| `z` | zoom the pane |
| `x` | close the pane (asks first) |
| `c` | new tab |
| `n` / `p`, `1`–`9` | next / previous tab, go to tab |
| `,` | rename the tab |
| space / `m` | tiled / main-and-stack layout |
| `:` or ⌘K | command palette |
| `?` | all keys |
| Ctrl-b | send a literal Ctrl-b |

In an empty pane, press `1`–`9` to start a profile, or Enter for a shell.
Drag the gaps between panes to resize them; double-click a gap to even them.

### Profiles

The built-in profiles are your login shell and each agent CLI that is
installed (`claude`, `codex`, `gemini`, `copilot`, `grok`, `opencode`,
`ollama`). Each runs unchanged, with its own login. Add or replace profiles in
`~/.laforge/forge/profiles.json`:

```json
[
  { "name": "aider", "label": "Aider", "description": "aider chat", "command": "aider", "agent": true },
  { "name": "logs", "label": "Logs", "command": "tail", "args": ["-f", "/var/log/system.log"] }
]
```

Panes inherit the environment `la up` was started with (minus variables that
would make an agent think it runs inside another agent), plus
`LAFORGE_SESSION`.

### Files

Everything is in `~/.laforge/forge` (`$LAFORGE_HOME` to change):
`forge.json` (the running forge's port, pid and token, mode 0600),
`forge.log`, `layout.json` and `profiles.json`.

## Embedding

LaForge is built to be mounted by a host program. Outcome runs it as
`out la …`:

```go
import laforge "github.com/RunOutcome/laforge/cli"

// in the host's command switch:
case "la":
    os.Exit(laforge.Main(laforge.Config{Prog: "out la", Reexec: []string{"la"}}, args[1:]))
```

| Package | What it is |
| --- | --- |
| `terminal` | pseudo-terminals (Unix PTY, Windows ConPTY), the replay ring, sessions with readers that rejoin without gaps |
| `forge` | the HTTP API, WebSocket streams and the embedded web client, as an `http.Handler` |
| `cli` | the `la` command tree as a library |
| `web` | the React client (`Forge` component, layout model) |

### API

All routes but `/api/health` need the token, as the cookie or
`Authorization: Bearer <token>`.

| Route | |
| --- | --- |
| `GET /api/health` | `{product, version, pid}` |
| `GET /api/profiles` | profiles, each with `available` |
| `GET /api/sessions` | every session |
| `POST /api/sessions` | `{profile, cwd?, title?, cols?, rows?}` starts one |
| `PATCH /api/sessions/{id}` | `{title}` |
| `DELETE /api/sessions/{id}` | stops it (`?force=1` to kill) and forgets it |
| `GET /api/sessions/{id}/stream` | WebSocket: binary output (the replay buffer first); send `{"type":"input","data":…}` or `{"type":"resize","cols":…,"rows":…}`; closes with "ended" when the session ends |
| `GET`/`PUT /api/layout` | the client's layout |
| `GET`/`PUT /api/config` | `{cwd}`: where new panes start |

## Licence

Apache-2.0. Copyright 2026 COR Holdings, LLC.
