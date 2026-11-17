# Program status in LaForge

A program in a pane says what it is doing; LaForge shows it on the pane, the
tab and the agents panel, notifies when it needs you, and gives it to
`la wait` and the event stream. LaForge keeps what was said. It never guesses
a status from screen text, and a program that reports nothing has none.

## OSC 7501

The wire form is the Program Status Protocol herdr-core reads, so a program
that reports to Herdr reports to LaForge unchanged:

    ESC ] 7501 ; state=<word>[:key=value…] (BEL | ESC \)

| Key | Meaning |
| --- | --- |
| `state` | `idle`, `working`, `done`, `blocked`, `error`; `clear` removes the record |
| `kind` | `permission`, `question` or `auth` (blocked only) |
| `progress` | 0–100 (working or blocked) |
| `id` | a child record, such as `build` or `tests/unit`; empty is the pane itself; `clear` with an id removes it and its children |
| `app` | the program's name for itself |
| `title`, `msg` | base64 |
| `src` | LaForge's own: `hook` for a report a hook made (others ignore it) |

A report with an unknown state is dropped; unknown keys are ignored. A
report may arrive split across reads. The status of a pane ends with its
program, or with its agent when the agent exits and leaves a shell.

## Without the escape code

- `la report working --msg "running the suite"` from inside a pane.
- `POST /api/sessions/{id}/status` with the same fields as JSON.
- Claude Code and Codex: `la hooks install` (their hooks call `la report --hook`).

## Shown words

`done` means finished while you were not looking: once a viewer looks at the
pane (`POST /api/sessions/{id}/seen`) it shows as `idle`. A hook's `working`
with 20 seconds of no output shows as `idle` (Claude Code runs no hook when
Esc interrupts a turn; a working agent draws). Urgency, for a tab or the
agents panel: blocked and error, then done, then working, then idle.
