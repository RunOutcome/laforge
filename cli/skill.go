// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package cli

import "strings"

// skill is what an agent reads to use LaForge: `la --skill`.
func skill(prog string) string {
	return strings.ReplaceAll(`# LaForge for agents

You are in a LaForge pane when $LAFORGE_SESSION is set. LaForge keeps
terminals running in panes a person watches in a browser. You can start
panes of your own, give them work, read them and wait for them.

## Run work in a pane
  la run make test                    stream it and wait (exit code is its own)
  la run --bg --name tests make test  start it, print its id, return
  la run --bg --name review claude "Review src/x.go. Read-only."
                                      an agent in a pane of its own
A plain la run checks in after 3 minutes and leaves the program running.

## Panes you start are your children
- They end when your pane ends, unless started with --outlive-parent.
- You are told (a line typed into your pane, starting [laforge]) when a
  child is blocked, errs, is done or ends; --no-notes turns that off.
- la ls --children lists them.

## Talk to a pane
  la send review "Focus on the error path."   type a line and press Enter
  la read review --lines 80                    its last lines as text
  la wait review                               until it needs attention or ends
  la wait --children --until ended             until every child has ended
  la close review                              end it
A send waits while a person is typing in that pane (up to a minute).
Name a pane by its id (s-…) or by a name only it has.

## Say what you are doing
The person sees each pane's status. If you are not an agent with LaForge
hooks, report it yourself:
  la report working --msg "running the suite"
  la report blocked --kind question --msg "Which database?"
  la report done
(Or print OSC 7501: ESC ] 7501 ; state=working ESC \.)

## Rules
- Read before you send; never type into a pane a person is using.
- Close the panes you started when you are done with them.
`, "la ", prog+" ")
}
