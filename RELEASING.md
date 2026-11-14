# Releasing LaForge

A release is a tag `vX.Y.Z` on `main`. The tag builds everything, signs it,
and creates a **draft** GitHub release; publishing the draft is a person's
decision and is what makes it public (and pushes the container image).

## Once: the signing key

Releases are signed with a Cosign key pair. The private key lives only in the
repository secrets; the public key is committed and embedded in the installer,
so `install.sh` can verify a release with plain `openssl`.

```sh
cd release
COSIGN_PASSWORD='<a strong passphrase>' cosign generate-key-pair   # cosign.key, cosign.pub
gh secret set COSIGN_PRIVATE_KEY -R RunOutcome/laforge < cosign.key
gh secret set COSIGN_PASSWORD -R RunOutcome/laforge          # paste the passphrase
sh embed-public-key.sh                                       # writes the key into install.sh
mv cosign.key <somewhere safe, off the repository>
git add cosign.pub install.sh && git commit -m "Release signing key"
```

`cosign.key` is never committed (`.gitignore` refuses it).

## Each release

1. `CHANGELOG.md`: move the changes under `## [X.Y.Z] - YYYY-MM-DD`.
2. Check locally: `make test`, `make release-check`, `make release-snapshot`
   (an unsigned build of every archive into `dist/`).
3. Tag and push:
   ```sh
   git tag -a vX.Y.Z -m "LaForge X.Y.Z" && git push origin vX.Y.Z
   ```
4. The `release` workflow tests, builds darwin/linux/windows × amd64/arm64,
   writes SBOMs and `checksums.txt`, signs it, and drafts the release with the
   changelog section as its notes. It refuses without the signing secrets or
   without a changelog section for the tag.
5. Check the draft: download `install.sh` and install from it
   (`sh install.sh --version vX.Y.Z --prefix /tmp/la-check`), run `la up`.
6. Publish the draft. The `image` workflow then pushes
   `ghcr.io/runoutcome/laforge:X.Y.Z` (and `:latest` unless it is a
   pre-release), which is the image a released `la up --container` runs.

Tags with a suffix (`v0.2.0-rc1`) are pre-releases.

## What a release contains

| Asset | What |
| --- | --- |
| `laforge_X.Y.Z_<os>_<arch>.tar.gz` / `.zip` | `la`, LICENSE, README, CHANGELOG, THIRD_PARTY_NOTICES |
| `*.sbom.json` | an SPDX SBOM per archive |
| `checksums.txt`, `checksums.txt.sig` | SHA-256 of every asset, and its signature |
| `cosign.pub` | the release public key |
| `install.sh` | the installer, with the key embedded |

## Keeping it current

- Go: the `toolchain` line in `go.mod` is what releases build with; move it
  to each Go security release (`go mod edit -toolchain=go1.X.Y`).
- Base images: the Dockerfile pins digests; refresh with
  `docker buildx imagetools inspect golang:1.27-bookworm` (and the node image).
- Bundled agent CLIs: `BUNDLED` in the Dockerfile pins versions; bump them
  deliberately.
- `scripts/notices.mjs` regenerates THIRD_PARTY_NOTICES.md after any
  dependency change; CI fails while it is stale.
