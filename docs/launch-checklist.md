# Launch & Discoverability Checklist

Operator playbook for the public launch. Grouped by tier (repo hygiene →
launch-ready → amplify). Each item lists status and the exact command or
steps. Items marked **OPERATOR** require a human decision, an account/credential,
or a design asset and cannot be automated from the codebase.

Repo: `lx-wnk/Agent-Dashboard` · License: MIT · Release tooling: GoReleaser
(`.goreleaser.yml` + `.github/workflows/release.yml`) — pushing a `v*` tag cuts a
release with notes generated from Conventional Commits.

---

## ⚠ Prerequisites (do these first — they gate the visual items)

- **Brand icons are placeholders.** `public/{icon-192,icon-512,apple-touch-icon}.png`
  are solid-blue fills, not a logo. The PWA maskable requirement is satisfied by
  the solid fill (mask-safe), but a real logo is needed before a hero GIF, social
  preview, or app screenshots look credible. **OPERATOR / design:** produce a logo,
  then replace the icons (and drop a dedicated padded maskable 512 into
  `public/`, updating the `purpose: 'maskable'` entry in `vite.config.ts`).
- **GitHub does not detect the LICENSE.** `LICENSE` is present and MIT, but
  `gh repo view --json licenseInfo` reports `none`. Re-check after the next push;
  if still undetected, ensure the file is exactly the SPDX MIT text with no
  leading content so GitHub's licensee classifier picks it up.

---

## Tier 0 — Repo hygiene (baseline discoverability)

| Item | Status | Action |
|---|---|---|
| Description | ✅ set | "Real-time monitoring and control dashboard for locally running Claude Code agents" |
| Topics | ✅ set (7) | `agent-monitoring, ai-agents, claude, claude-code, dashboard, developer-tools, mcp` — consider adding `pwa`, `self-hosted`, `sse` |
| Homepage URL | ❌ empty | **OPERATOR:** decide the URL. This is a local-first app with no hosted instance — point it at the docs site if one exists, else leave empty. `gh repo edit lx-wnk/Agent-Dashboard --homepage "https://..."` |
| License detection | ⚠ see prereq | Verify GitHub shows "MIT" on the repo page after next push |

Add topics:
```sh
gh repo edit lx-wnk/Agent-Dashboard \
  --add-topic pwa --add-topic self-hosted --add-topic sse
```

---

## Tier 1 — Launch-ready

### First release tag — **OPERATOR (irreversible: publishes a public release)**
GoReleaser is wired; a `v*` tag triggers the release workflow.
```sh
# Pick the version deliberately (0.1.0 = first public preview).
git checkout upcoming && git pull
git tag -a v0.1.0 -m "v0.1.0 — first public preview"
git push origin v0.1.0        # → release.yml runs GoReleaser
```
Before tagging: confirm `.goreleaser.yml` targets the intended binaries/artifacts
and that `CHANGELOG.md [Unreleased]` is curated (GoReleaser generates notes from
commits, but the human-written Unreleased section is the reference).

### Hero demo GIF in README — **OPERATOR (needs the running app + real branding)**
- Record a 10–20s loop of the core flow (agent roster → task pipeline board →
  answering an AskUserQuestion inline). Tools: `asciinema`+`agg` for the CLI, or a
  screen recorder → GIF for the UI. Keep < 5 MB, ~1200px wide.
- Commit to `docs/assets/hero.gif` and embed near the top of `README.md`:
  `![Agent Dashboard](docs/assets/hero.gif)`
- There are static screenshots under `docs/local/Post/` (gitignored) usable as a
  starting point.

### `good-first-issue` labels + curated issues
Create the labels, then apply them to 3–5 genuinely small, well-scoped issues.
```sh
gh label create "good first issue" --color 7057ff \
  --description "Good for newcomers" --force
gh label create "help wanted" --color 008672 \
  --description "Extra attention is welcome" --force
# then, per chosen issue:
gh issue edit <N> --add-label "good first issue"
```
**OPERATOR:** pick the issues. Candidates (small, self-contained) from the current
backlog: the maskable/branding asset swap, additional `pnpm build` doc polish,
adding topics/`homepageUrl`, and small a11y follow-ups.

### Social preview image (1280×640) — **OPERATOR (design + upload, not a repo file)**
GitHub's social preview is a repo *setting*, not a committed file.
- Design a 1280×640 PNG: logo + product name + one-line value prop on the brand
  background (`#0f172a`). Needs the real logo (see prereqs).
- Upload: repo **Settings → General → Social preview → Upload an image** (no `gh`
  command for this; the web UI or the GraphQL `updateRepository` mutation).

---

## Tier 2 — Amplify (post-launch)

### Awesome-list PRs — **OPERATOR (external repos)**
Submit to lists where this fits; each has its own contribution format:
- `awesome-claude` / `awesome-claude-code` (Claude ecosystem)
- `awesome-ai-agents`
- `awesome-selfhosted` (local-first, self-hosted dashboard)
- `awesome-devtools`

### Launch posts — **OPERATOR (accounts + timing)**
Draft copy (tune before posting):

> **Show HN / Reddit r/selfhosted title:** "Agent Dashboard — a local-first,
> real-time monitor & control plane for Claude Code agents"
>
> Body: Runs entirely on `127.0.0.1`, reads your local `~/.claude` session logs
> over SSE, and shows every agent's tokens, cost, tool activity, tasks and
> subagents live — plus a task pipeline you can drive and an authenticated MCP
> control plane. No telemetry, MIT-licensed, Go + Vue. Optional macOS desktop
> app. Feedback welcome.

Suggested venues: Hacker News (Show HN), r/selfhosted, r/LocalLLaMA, the Anthropic
/ Claude community channels, X/Mastodon dev threads.

### Name consistency (optional)
Repo is `Agent-Dashboard`; product name in UI/README is "Agent Dashboard". Keep the
hyphenless display name consistent across the release title, posts, and any listing.

---

## What was done in code (this PR)

- **SEO-P3-1b:** added the `purpose: 'maskable'` 512 icon entry to the PWA manifest
  in `vite.config.ts` (solid fill is mask-safe; satisfies the Lighthouse/PWA
  maskable requirement). No new asset — see the branding prerequisite above.
- This checklist itself.

Everything else above is operator/design/account work that cannot be committed
from the codebase.
