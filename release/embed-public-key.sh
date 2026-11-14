#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: 2026 COR Holdings, LLC
#
# Embeds release/cosign.pub in release/install.sh, so the installer trusts
# exactly the key that signs releases.
#
#   sh release/embed-public-key.sh           # write it
#   sh release/embed-public-key.sh --check   # fail if install.sh differs
#   sh release/embed-public-key.sh --key F --out F2   # another key/output (tests)
set -eu
dir=$(cd "$(dirname "$0")" && pwd)
key="$dir/cosign.pub"; in="$dir/install.sh"; out="$in"; check=0
while [ $# -gt 0 ]; do
  case "$1" in
    --check) check=1; shift ;;
    --key) key=$2; shift 2 ;;
    --out) out=$2; shift 2 ;;
    *) echo "unknown option $1" >&2; exit 2 ;;
  esac
done
[ -s "$key" ] || { echo "no public key at $key (see RELEASING.md: create the signing key)" >&2; exit 1; }
grep -q 'BEGIN PUBLIC KEY' "$key" || { echo "$key is not a PEM public key" >&2; exit 1; }
tmp=$(mktemp)
awk -v keyfile="$key" '
  /^# BEGIN COSIGN PUBLIC KEY$/ { print; printf "PUBKEY='\''"; first = 1
    while ((getline line < keyfile) > 0) { if (!first) printf "\n"; printf "%s", line; first = 0 }
    print "'\''"; skip = 1; next }
  /^# END COSIGN PUBLIC KEY$/ { skip = 0 }
  !skip { print }
' "$in" > "$tmp"
if [ "$check" = 1 ]; then
  if cmp -s "$tmp" "$in"; then rm -f "$tmp"; echo "install.sh embeds $key"; exit 0; fi
  rm -f "$tmp"; echo "install.sh does not embed $key; run: sh release/embed-public-key.sh" >&2; exit 1
fi
mv "$tmp" "$out"; chmod 0755 "$out"
echo "embedded $key in $out"
