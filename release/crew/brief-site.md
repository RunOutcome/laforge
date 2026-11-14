# Brief: getlaforge.com for the LaForge release

You are **site** on the LaForge v0.1.0 release crew. The lead is the Claude
session working in ~/projects/outcome-workspace (find it with ListAgents);
**media** makes the screenshots, GIF and social cards.

**Your worktree:** this folder, branch `laforge-release-site` of the
corholdings repository. The site is `getlaforge.com/` (content in
`src/content`, settings in `site.json`), built by `./laforge` at the root:
`./laforge check getlaforge.com`, `./laforge build getlaforge.com`,
`./laforge serve getlaforge.com --port 4670`. Read the LaForge product:
~/projects/laforge/outcome/laforge (README.md, CHANGELOG.md, RELEASING.md,
release/crew/CHECKLIST.md: rows 5–8 are yours).

## Deliver

1. getlaforge.com becomes the product's home, keeping the writing (posts,
   authors, crew, feeds, URLs all keep working, under Writing in the nav):
   - landing: headline and the one-liner "Many shells and agents, side by side,
     in your browser."; the hero screenshot; `curl … | sh` then `la up`;
     what it does (tabs and splits, tmux keys, panes survive the browser,
     agents: Claude Code, Codex, Antigravity, Qwen Code, Copilot, Grok,
     opencode, Ollama; exit-to-shell; the container); a screenshot strip;
     open source (Apache-2.0) and the GitHub link https://github.com/RunOutcome/laforge;
   - `/download` (or `/install` as a page): the installer, what it verifies,
     verify by hand (openssl and sha256sum lines from RELEASING/.goreleaser),
     the archives per platform, the container (`ghcr.io/runoutcome/laforge`,
     `la up --container`), Windows experimental, links to the release page;
   - `site.json`: tagline, description and organization text describe the
     product; OG image from media's `og.png`.
2. Images: once media says they're pushed (branch `release/media` in the
   laforge repo, `docs/media/`), copy them to
   `getlaforge.com/src/assets/laforge/` (README links point at
   `https://getlaforge.com/assets/laforge/<file>`: keep those names), with
   WebP versions and width/height on every <img>.
3. `/install` (the curl target): today it serves the OLD LaForge installer.
   Prepare the change to serve the new one (the release's `install.sh`; the
   GitHub "latest/download/install.sh" URL) but keep it a separate commit, and
   flag it in your report: it replaces the old product and is the founder's go.

## Rules

- Match the existing look (dark #071018, accent #63c9ff); the site's own
  build tool, no new framework.
- `./laforge check` and `build` must pass. Don't `deploy` or `publish`, don't
  push to main, don't touch the founder's uncommitted files in the main
  checkout. Commit on `laforge-release-site`, push the branch.
- Chrome belongs to media until media tells you it's done; until then check
  your pages with the build and curl against `serve`.
- Stop only processes you started, by PID; never touch ports 4646 or 8777.
- When done: SendMessage the lead with the commit, the pages changed, and
  open questions for the founder (as questions, not decisions made on the founder's behalf).
