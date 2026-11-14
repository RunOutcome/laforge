#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: 2026 COR Holdings, LLC
#
# Installs la (LaForge) from a signed release.
#
#   curl -fsSL https://github.com/RunOutcome/laforge/releases/latest/download/install.sh | sh
#   sh install.sh [--version vX.Y.Z] [--prefix DIR] [--base URL] [--force]
#
# It verifies checksums.txt against its signature with the release public key
# embedded below (openssl, or cosign if openssl is missing), then the archive
# against checksums.txt, and only then installs la as <prefix>/la-<version>
# with <prefix>/la pointing at it. It needs no root, starts nothing, and
# refuses to replace an `la` it did not install unless --force is given.
#
#   --version  a release tag (default: the latest release)
#   --prefix   where to install (default: ~/.local/bin)
#   --base     download from this https:// (or file://) URL instead of
#              GitHub: a mirror holding <base>/<tag>/<asset>; needs --version
#   --force    replace an existing la that is not LaForge's
#
# While the repository is private, downloads go through the GitHub CLI
# (`gh auth login` first).
set -eu

REPO="RunOutcome/laforge"
VERSION=""
PREFIX="${HOME}/.local/bin"
BASE=""
FORCE=0

say() { printf '%s\n' "$*"; }
fail() { printf 'install.sh: %s\n' "$*" >&2; exit 1; }

while [ $# -gt 0 ]; do
  case "$1" in
    --version) VERSION="${2:-}"; shift 2 ;;
    --prefix) PREFIX="${2:-}"; shift 2 ;;
    --base) BASE="${2:-}"; shift 2 ;;
    --force) FORCE=1; shift ;;
    -h|--help) sed -n '5,22p' "$0"; exit 0 ;;
    *) fail "unknown option $1 (see --help)" ;;
  esac
done

# --- the release public key (release/embed-public-key.sh writes this block) ---
# BEGIN COSIGN PUBLIC KEY
PUBKEY='-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE73HF9M4ZrMe+lMpqgjRKsIr6QjSo
7bX5nJ3ymU4pSSyRoz3lfzodn3brxUWhnkMG8F6hLd+SHOMNzS4a44SbRQ==
-----END PUBLIC KEY-----'
# END COSIGN PUBLIC KEY

[ -n "$PUBKEY" ] || fail "this copy of install.sh has no release public key embedded, so it cannot verify anything. Use the install.sh attached to the release."

case "$(uname -s)" in
  Darwin) OS=darwin ;;
  Linux) OS=linux ;;
  *) fail "this installer is for macOS and Linux; on Windows unzip la.exe from the release" ;;
esac
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  arm64|aarch64) ARCH=arm64 ;;
  *) fail "unsupported CPU $(uname -m)" ;;
esac

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT INT TERM

USE_GH=0
if [ -z "$BASE" ] && command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
  USE_GH=1
fi

if [ -z "$VERSION" ]; then
  if [ "$USE_GH" = 1 ]; then
    VERSION=$(gh release view -R "$REPO" --json tagName -q .tagName 2>/dev/null) || fail "could not find the latest release of $REPO"
  elif [ -n "$BASE" ]; then
    fail "with --base, pass --version too"
  else
    VERSION=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest" | sed 's#.*/tag/##') || true
    case "$VERSION" in v*) ;; *) fail "could not find the latest release (is the repository private? run gh auth login, or pass --version)" ;; esac
  fi
fi
case "$VERSION" in v[0-9]*) ;; *) fail "--version must look like v1.2.3, not $VERSION" ;; esac
NUM=${VERSION#v}
ARCHIVE="laforge_${NUM}_${OS}_${ARCH}.tar.gz"

fetch() { # asset...
  for asset in "$@"; do
    if [ "$USE_GH" = 1 ]; then
      gh release download "$VERSION" -R "$REPO" -p "$asset" -D "$WORK" --clobber >/dev/null 2>&1 || fail "could not download $asset from $REPO $VERSION"
    else
      url="${BASE:-https://github.com/$REPO/releases/download}/$VERSION/$asset"
      case "$url" in
        file://*) cp "${url#file://}" "$WORK/$asset" 2>/dev/null || fail "could not read $url" ;;
        *) curl -fsSL --proto '=https' --tlsv1.2 -o "$WORK/$asset" "$url" || fail "could not download $url" ;;
      esac
    fi
  done
}

say "LaForge $VERSION for $OS/$ARCH"
fetch checksums.txt checksums.txt.sig "$ARCHIVE"

# --- verify the signature on checksums.txt ---
KEY="$WORK/cosign.pub"
printf '%s\n' "$PUBKEY" > "$KEY"
if command -v openssl >/dev/null 2>&1; then
  openssl base64 -d -A -in "$WORK/checksums.txt.sig" -out "$WORK/checksums.txt.sig.der" 2>/dev/null || fail "the signature file is not base64"
  openssl dgst -sha256 -verify "$KEY" -signature "$WORK/checksums.txt.sig.der" "$WORK/checksums.txt" >/dev/null 2>&1 \
    || fail "SIGNATURE VERIFICATION FAILED: checksums.txt was not signed by the LaForge release key, or was altered. Nothing was installed."
elif command -v cosign >/dev/null 2>&1; then
  cosign verify-blob --key "$KEY" --signature "$WORK/checksums.txt.sig" --insecure-ignore-tlog=true "$WORK/checksums.txt" >/dev/null 2>&1 \
    || fail "SIGNATURE VERIFICATION FAILED (cosign). Nothing was installed."
else
  fail "neither openssl nor cosign is available to verify the release; install openssl and retry"
fi
say "  signature ok"

# --- verify the archive against checksums.txt ---
expected=$(awk -v f="$ARCHIVE" '$2 == f { print $1 }' "$WORK/checksums.txt")
[ -n "$expected" ] || fail "$ARCHIVE is not listed in checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$WORK/$ARCHIVE" | awk '{print $1}')
else
  actual=$(shasum -a 256 "$WORK/$ARCHIVE" | awk '{print $1}')
fi
[ "$expected" = "$actual" ] || fail "CHECKSUM MISMATCH for $ARCHIVE. Nothing was installed."
say "  checksum ok"

# --- install ---
tar -xzf "$WORK/$ARCHIVE" -C "$WORK" la || fail "the archive holds no la"
mkdir -p "$PREFIX"
if [ -e "$PREFIX/la" ] || [ -L "$PREFIX/la" ]; then
  current=$(readlink "$PREFIX/la" 2>/dev/null || true)
  case "$current" in
    la-v*) ;; # ours
    *)
      if [ "$FORCE" != 1 ]; then
        fail "$PREFIX/la already exists and is not LaForge's (${current:-a file}). Move it aside, or pass --force to replace it."
      fi
      say "  replacing $PREFIX/la (${current:-a file})"
      ;;
  esac
fi
install -m 0755 "$WORK/la" "$PREFIX/la-$VERSION"
ln -sfn "la-$VERSION" "$PREFIX/la.new" && mv -f "$PREFIX/la.new" "$PREFIX/la"
say "  installed $PREFIX/la -> la-$VERSION"

case ":$PATH:" in
  *":$PREFIX:"*) ;;
  *) say ""; say "$PREFIX is not on your PATH. Add it to your shell profile:"; say "  export PATH=\"$PREFIX:\$PATH\"" ;;
esac
say ""
say "Run: la up"
