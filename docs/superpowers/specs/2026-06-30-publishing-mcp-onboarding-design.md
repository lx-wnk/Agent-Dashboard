# Publishing & MCP Onboarding — Design Spec

> Date: 2026-06-30 · Status: Approved · Branch: `feat/publishing-mcp-onboarding` (off `upcoming`)
> User initiative: make the dashboard easy to install for non-developers AND trivial to connect to Claude, and ensure a real release can ship. Mostly config + docs (+ a small key-dialog tweak).

## Why

The release machinery already exists (`.goreleaser.yml` + `.github/workflows/release.yml` on `v*` tags → cross-platform binaries with the embedded SPA) and the MCP server is mature + documented (`docs/guides/mcp.md`, scoped-key dialog). But: no `v*` tag was ever cut (no published binary), the Quickstart is dev-clone-heavy (Go/Task/air/pnpm prereqs) with no end-user install path, goreleaser only emits raw binaries (no brew/docker/checksums), and connecting the MCP to Claude is a copy-an-example flow rather than a one-command add. This initiative closes the install + connect ergonomics so a non-developer can download, run, and connect.

## Decisions (user-approved)

| # | Decision | Rationale |
|---|---|---|
| D1 | Expand `.goreleaser.yml`: per-OS archives, checksums, Homebrew tap formula, Docker image | More install channels from one release; the single embedded-frontend binary is the artifact. |
| D2 | Add an end-user install guide; restructure README to lead with binary/brew/docker; move dev-setup to CONTRIBUTING | Non-developers should not need Go/Task/air/pnpm to run it. |
| D3 | One-command MCP connect (`claude mcp add …`) + key-dialog copies that exact command | Remove the copy-an-example friction. |
| D4 | Release-readiness dry-run + `docs/RELEASING.md` | Ensure a `vX.Y.Z` tag actually succeeds; document the (operator) release steps. |
| D5 | Do NOT cut/push the release tag | Outward-facing, irreversible operator action — the user triggers it. |

## Scope

In: `.goreleaser.yml` expansion (archives/checksums/brews/dockers) + a local `--snapshot` dry-run; `docs/guides/install.md` (binary/brew/docker/build) + README/Quickstart restructure + CONTRIBUTING dev-setup; MCP-connect one-liner in `docs/guides/mcp.md` + the key-dialog copy-command update (frontend); `docs/RELEASING.md`; CHANGELOG.

Out: pushing the `v0.1.0` tag (operator); broader SEO/discoverability (repo About, awesome-lists, launch posts); Windows (unsupported); any server runtime-logic change beyond the key-dialog copy string.

## Architecture / changes

### 1. `.goreleaser.yml` (expand)
- Read the current config (builds + the `before` hook that runs `pnpm install --frozen-lockfile` + `pnpm build` into `server/frontend/dist`). Keep that hook.
- **builds:** darwin + linux × amd64 + arm64, `-ldflags "-X main.version={{.Version}}"`, `main: ./cmd/serve`, binary `agent-dashboard`, CGO disabled (modernc/sqlite is pure-Go — confirm).
- **archives:** tar.gz per target (name template incl. os/arch), include README + LICENSE.
- **checksum:** `checksums.txt` (sha256).
- **brews:** a Homebrew formula published to a tap repo `lx-wnk/homebrew-tap` (the user creates that repo once); formula installs the binary, caveats note it binds `127.0.0.1` and reads `~/.claude`.
- **dockers:** an image built FROM the goreleaser-built linux binary (distroless/static base; the binary already embeds the SPA). Document run with `-p 127.0.0.1:13120:13120` and a `~/.claude` read mount; security caveat that it exposes sensitive Claude session data → keep loopback, never publish the port.
- Verify with `goreleaser release --snapshot --clean` (no publish) locally; fix config until the snapshot builds all targets + the docker image.

### 2. End-user install (`docs/guides/install.md` + README + CONTRIBUTING)
- `install.md`:
  - **Binary:** download the OS/arch archive from the latest GitHub release, extract, `chmod +x`, run `./agent-dashboard serve`; PATH note.
  - **Homebrew:** `brew install lx-wnk/tap/agent-dashboard` (after the tap exists).
  - **Docker:** `docker run --rm -p 127.0.0.1:13120:13120 -v ~/.claude:/root/.claude:ro ghcr.io/lx-wnk/agent-dashboard` (adjust to the real image ref + config-dir path; security caveat).
  - **From source (dev):** point to CONTRIBUTING.
  - Prerequisite for ALL: Claude Code itself (the dashboard reads its session data); macOS/Linux only.
- README: restructure the Quickstart to lead with the binary/brew install for users; move the Go/Task/air/pnpm dev prerequisites into a "Develop / from source" pointer to CONTRIBUTING. Keep one obvious "run it" path at the top.
- CONTRIBUTING: ensure the dev-setup (clone + `task setup` + `task dev`) lives here.

### 3. MCP-connect onboarding (`docs/guides/mcp.md` + key dialog)
- Verify the current `claude mcp add` syntax for an HTTP transport with a bearer header (against current Claude Code) and document the exact one-liner, e.g.:
  `claude mcp add agent-dashboard --transport http http://127.0.0.1:13120/api/mcp --header "Authorization: Bearer <KEY>"`
  (confirm flag names; adapt to the real CLI). Include a project-scoped variant if relevant.
- Update the key-dialog copy-command (frontend component that currently copies an export/example) to emit that exact `claude mcp add` command with the generated key substituted. Find the component + the current copied string; replace it; keep the masked-key handling.
- A focused "Connect the dashboard to Claude" subsection: generate a scoped key in the UI → run the copied command → the session auto-connects.

### 4. Release-readiness (`docs/RELEASING.md`)
- Run the goreleaser snapshot; record + fix any config errors so a real tag works.
- `RELEASING.md`: version scheme (semver, `vX.Y.Z`), the steps (update CHANGELOG `[Unreleased]`→version, tag, push → the Release workflow runs goreleaser), the one-time `homebrew-tap` repo setup + any token/secret the brew/docker publish needs (e.g. a tap-push token, GHCR login), and how the changelog feeds the release notes.

## Data flow / behavior
- No runtime data-flow change. Build/release flow: tag `v*` → workflow → goreleaser (`before`: build SPA → embed) → binaries + archives + checksums + brew formula + docker image published to the GitHub release / tap / registry.
- MCP connect: UI key dialog → copy `claude mcp add …` → user runs it → Claude session connects to `POST /api/mcp` with the bearer key.

## Error handling / edge cases
- goreleaser snapshot must build ALL targets; a failing target (e.g. CGO needed) → fix the build config (confirm modernc sqlite = pure Go, CGO_ENABLED=0).
- brew/docker publish needs credentials only at real-release time (not snapshot) — document, don't block the dry-run.
- Docker image reads `~/.claude` → document read-only mount + loopback-only; never bind 0.0.0.0.
- `claude mcp add` syntax drift across CC versions → document the verified version + a fallback to the JSON config.

## Testing / verification
- `goreleaser release --snapshot --clean` builds all archives + checksums + the docker image locally (the primary gate; no publish).
- Key-dialog change: `pnpm typecheck` + `pnpm lint` + the existing key-dialog test updated to assert the new copied command string.
- Doc accuracy: every command in install.md/mcp.md/RELEASING.md is runnable as written; internal links valid.
- Full `go build ./...` (binary still builds) + `pnpm build` (SPA still embeds) sanity.

## Risks / notes
- Homebrew tap requires a separate `lx-wnk/homebrew-tap` repo — one-time, documented; the formula push is part of the real release, not the snapshot.
- The Docker image surfaces sensitive Claude data — the docs MUST stress loopback-only + read-only mount; this matches the dashboard's existing 127.0.0.1 bind invariant.
- Cutting `v0.1.0` is intentionally NOT done here (operator action). This slice makes the tag-push succeed and the install/connect ergonomics good.
- modernc/sqlite is pure-Go → CGO can stay disabled for clean cross-compiles; confirm during the snapshot.
