#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: 2026 COR Holdings, LLC
#
# Installs the agent CLIs the image does not carry (ON_FIRST_START) into the
# home volume, in the background, so the forge is up at once; their panes turn
# available in the picker when the install finishes. Set ON_FIRST_START="" to
# skip. Progress: ~/.laforge/forge/agents-install.log
set -e
mkdir -p "$LAFORGE_HOME" "$HOME/.local/bin"
[ -f "$HOME/.zshrc" ] || : > "$HOME/.zshrc"   # no zsh first-run wizard
if [ -n "$ON_FIRST_START" ]; then
  missing=""
  for pkg in $ON_FIRST_START; do
    [ -d "$HOME/.local/lib/node_modules/$pkg" ] || missing="$missing $pkg"
  done
  if [ -n "$missing" ]; then
    ( echo "installing:$missing"; npm install -g --no-fund --no-audit $missing && echo "done" || echo "failed" ) \
      >> "$LAFORGE_HOME/agents-install.log" 2>&1 &
  fi
fi
# Antigravity (agy) ships as a native binary from Google's own installer,
# which puts it in ~/.local/bin.
if [ "${INSTALL_ANTIGRAVITY:-1}" != "0" ] && [ ! -x "$HOME/.local/bin/agy" ]; then
  ( echo "installing: antigravity (agy)"
    curl -fsSL https://antigravity.google/cli/install.sh | bash && echo "done" || echo "failed" ) \
    >> "$LAFORGE_HOME/agents-install.log" 2>&1 &
fi
exec "$@"
