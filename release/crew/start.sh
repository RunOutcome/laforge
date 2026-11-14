#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: 2026 COR Holdings, LLC
#
# Starts the LaForge release crew, in LaForge: a forge of its own (port 4650,
# home ~/.laforge-crew, so your everyday forge on 4646 is untouched) with one
# Claude Code pane per crew member, each in its own git worktree, started on
# its brief. Run it again any time: it reuses the worktrees, the forge and any
# crew pane still running, and opens the forge.
#
#   release/crew/start.sh            start (or reopen) the crew
#   release/crew/start.sh --dry-run  show what it would do
#   release/crew/start.sh --stop     stop the crew forge (and its panes)
#
# Crew:  media  docs/media + README    laforge worktree, branch release/media
#        site   getlaforge.com         corholdings worktree, branch laforge-release-site
set -euo pipefail

REPO=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
CORH="${CORHOLDINGS:-$HOME/projects/corholdings}"
CREW_HOME="${LAFORGE_CREW_HOME:-$HOME/.laforge-crew}"
PORT="${LAFORGE_CREW_PORT:-4650}"
MEDIA_WT="$REPO.worktrees/release-media"
SITE_WT="$CORH.worktrees/laforge-release-site"
DRY=0

say() { printf '\033[1m%s\033[0m\n' "$*"; }
run() { if [ "$DRY" = 1 ]; then echo "  would run: $*"; else "$@"; fi; }
die() { printf 'start.sh: %s\n' "$*" >&2; exit 1; }

case "${1:-}" in
  --dry-run) DRY=1 ;;
  --stop) LAFORGE_HOME="$CREW_HOME" "$REPO/bin/la" down; exit ;;
  "") ;;
  *) die "unknown option $1 (see the top of this file)" ;;
esac

for tool in git curl claude make; do command -v "$tool" >/dev/null || die "needs $tool on PATH"; done
[ -d "$CORH/.git" ] || die "no corholdings repository at $CORH (set CORHOLDINGS)"

say "1. la, built from $REPO"
run make -C "$REPO" build >/dev/null
LA="$REPO/bin/la"

say "2. worktrees"
worktree() { # repo dir branch base
  if [ -d "$2" ]; then echo "  $2 (exists)"; return; fi
  git -C "$1" fetch -q origin 2>/dev/null || true
  if git -C "$1" show-ref -q --verify "refs/heads/$3"; then
    run git -C "$1" worktree add -q "$2" "$3"
  else
    run git -C "$1" worktree add -q -b "$3" "$2" "$4"
  fi
  echo "  $2 ($3)"
}
worktree "$REPO" "$MEDIA_WT" release/media main
worktree "$CORH" "$SITE_WT" laforge-release-site main
# the media agent runs its demo forge from its own build
[ "$DRY" = 1 ] || make -C "$MEDIA_WT" build >/dev/null

say "3. crew profiles in $CREW_HOME"
brief() { # name worktree
  printf 'You are %s on the LaForge v0.1.0 release crew. Read your brief at %s and the checklist at %s, then do the work. Ask the lead (ListAgents, SendMessage) when blocked; never ask in this terminal.' \
    "$1" "$REPO/release/crew/brief-$1.md" "$REPO/release/crew/CHECKLIST.md"
}
if [ "$DRY" = 0 ]; then
  mkdir -p "$CREW_HOME"
  python3 - "$CREW_HOME/profiles.json" "$(command -v claude)" "$(brief media)" "$(brief site)" "$REPO" <<'PY'
import json, sys
path, claude, media, site, repo = sys.argv[1:]
def crew(name, label, prompt):
    return {"name": name, "label": label, "description": "LaForge release crew: " + name,
            "command": claude, "args": ["--name", "laforge-" + name, "--permission-mode", "acceptEdits", "--add-dir", repo, "--", prompt], "agent": True}
json.dump([crew("crew-media", "Media", media), crew("crew-site", "Site", site)], open(path, "w"), indent=2)
PY
fi
echo "  crew-media, crew-site"

say "4. the crew forge on port $PORT"
export LAFORGE_HOME="$CREW_HOME" LAFORGE_NO_BROWSER=1
run "$LA" up --port "$PORT" --cwd "$MEDIA_WT" --no-open >/dev/null
if [ "$DRY" = 1 ]; then echo "  would start panes Media (in $MEDIA_WT) and Site (in $SITE_WT)"; exit 0; fi
STATE=$(cat "$CREW_HOME/forge.json")
URL=$(printf '%s' "$STATE" | python3 -c 'import json,sys; print(json.load(sys.stdin)["url"])')
TOKEN=$(printf '%s' "$STATE" | python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])')
api() { curl -fsS -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' "$@"; }

say "5. crew panes"
running=$(api "$URL/api/sessions")
pane() { # profile title cwd
  if printf '%s' "$running" | python3 -c 'import json,sys; t=sys.argv[1]; sys.exit(0 if any(s.get("title")==t and s.get("state") in ("starting","running") for s in json.load(sys.stdin)) else 1)' "$2"; then
    echo "  $2: already running"
  else
    api -X POST "$URL/api/sessions" -d "{\"profile\":\"$1\",\"title\":\"$2\",\"cwd\":\"$3\",\"cols\":160,\"rows\":48}" >/dev/null
    echo "  $2: started in $3"
  fi
}
pane crew-media Media "$MEDIA_WT"
pane crew-site Site "$SITE_WT"

say "6. open"
unset LAFORGE_NO_BROWSER
"$LA" open
cat <<MSG

The crew is in the forge, in the tab "Found" (Media, Site; rename it with
the pencil). Their tool prompts wait for you there: approving them is yours. Checklist: release/crew/CHECKLIST.md.
Stop it with: release/crew/start.sh --stop
MSG
