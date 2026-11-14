# LaForge v0.1.0 release checklist

Owners: **you** (the founder), **lead** (the release-lead session),
**media** and **site** (the crew agents started by `release/crew/start.sh`).
State comes from branches, merges and builds; this file lists the work, it is
not a status log.

## Done (packaging)

- Code reviewed twice, all findings fixed; race tests on Linux/macOS, tests on
  Windows (runner and CI), govulncheck clean on Go 1.27.4.
- GoReleaser config, signed checksums, SBOMs, draft-only releases.
- `install.sh` verified end to end (good, tampered, wrong key, foreign `la`).
- CI, release and image workflows green on GitHub (release in snapshot mode).
- THIRD_PARTY_NOTICES, CHANGELOG, RELEASING.md; pinned image bases and CLIs.

## Dress-up (crew)

| # | Work | Owner | Lands in |
|---|------|-------|----------|
| 1 | Capture a demo forge with no personal data: hero screenshot (3–4 panes: Claude Code, Codex, a shell, a split), New pane menu, ⌘K palette, light mode, container panes | media | `docs/media/` (laforge repo, branch `release/media`) |
| 2 | A 10–20 s demo GIF/MP4: `la up`, split, start an agent, close and reopen the browser, panes still there | media | `docs/media/` |
| 3 | OG / social card 1200×630 and a GitHub social preview 1280×640 in the site's look | media | `docs/media/` |
| 4 | README: shields.io badges (licence, release, CI, Go, platforms, container), hero screenshot, one GIF, served from `https://getlaforge.com/assets/laforge/` | media | laforge `release/media` |
| 5 | getlaforge.com becomes the product's home: landing (headline, hero, the three-line `la up` story, agents supported, container), screenshots, Install/Download page (install.sh, checksums, verify by hand, container, Windows experimental), link to GitHub; the writing stays | site | corholdings branch `laforge-release-site` |
| 6 | Site assets: media's images copied to `getlaforge.com/src/assets/laforge/`, optimized (WebP + PNG fallback, width/height set) | site | corholdings `laforge-release-site` |
| 7 | `/install` on getlaforge.com serves the **new** installer (redirect to the release's `install.sh`), behind your go | site (prepare), you (approve) | corholdings |
| 8 | `./laforge check getlaforge.com` and `build` pass; local preview reviewed | site, lead | — |
| 9 | Review both branches, merge, rebuild | lead | laforge `main`, corholdings `main` |

## Yours (public, money or keys)

1. **Signing key** (once):
   ```sh
   cd ~/projects/laforge/outcome/laforge/release
   COSIGN_PASSWORD='<passphrase>' cosign generate-key-pair
   gh secret set COSIGN_PRIVATE_KEY -R RunOutcome/laforge < cosign.key
   gh secret set COSIGN_PASSWORD -R RunOutcome/laforge
   sh embed-public-key.sh && mv cosign.key ~/<safe place>
   git add cosign.pub install.sh && git commit -m "Release signing key" && git push
   ```
2. **Positioning:** getlaforge.com moves from the studio blog to the product's
   home (the writing stays, under Writing). The old installer at
   getlaforge.com/install is replaced by the new `la`.
3. **Go public:** make `RunOutcome/laforge` public (the README badges and the
   installer's anonymous download need it).
4. **Release:** set the date in CHANGELOG.md, `git tag -a v0.1.0 -m "LaForge 0.1.0" && git push origin v0.1.0`,
   check the draft, publish it (pushes `ghcr.io/runoutcome/laforge:0.1.0`;
   make the package public in GitHub's package settings).
5. **Deploy the site:** `~/projects/corholdings/getlaforge.com/scripts/deploy.sh`
   (after 3 and 4, so its links work).
