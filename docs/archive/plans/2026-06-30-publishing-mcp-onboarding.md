# Plan: Publishing & MCP Onboarding

**Spec:** `docs/superpowers/specs/2026-06-30-publishing-mcp-onboarding-design.md`
**Branch:** `feat/publishing-mcp-onboarding` (off `upcoming`)
**Goal:** Make the dashboard installable by non-developers (binary/brew/docker) and trivially connectable to Claude (one-command MCP add), and ensure a real release tag would succeed end-to-end.

## Architecture

No runtime changes. Build/release flow: `v*` tag → `release.yml` → GoReleaser:
1. `before` hooks build the Vue SPA (`pnpm install && pnpm build`) into `server/frontend/dist`
2. Go builds embed the SPA (`go:embed`) → self-contained binary per target
3. GoReleaser publishes: archives + checksums to GitHub release, Homebrew formula to `lx-wnk/homebrew-tap`, Docker images to `ghcr.io/lx-wnk/agent-dashboard`

## Tech Stack

GoReleaser v2, Docker buildx (multi-arch), GitHub Container Registry (GHCR), Homebrew tap, Vue 3 TypeScript (key-dialog docs only — frontend code needs no change)

---

## Pre-read: Current State Summary

**`.goreleaser.yml` already has:**
- `builds`: darwin+linux × amd64+arm64, `CGO_ENABLED=0`, `main: ./cmd/serve`, ldflags `-X main.version={{ .Version }}` — confirmed correct
- `archives`: tar.gz with name template `{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}`, includes LICENSE + README.md + PRIVACY.md + THIRD_PARTY_LICENSES.md — already complete
- `checksum`: `checksums.txt` (sha256) — already present
- `release.footer` references `install.sh` (already in repo root) — no change needed
- **MISSING**: `brews` stanza, `dockers` stanza, `docker_manifests` stanza

**`release.yml` current:** runs goreleaser on `v*` push, permissions `contents: write` only; has no GHCR login step, no `HOMEBREW_TAP_TOKEN` env var. These must be added.

**CGO:** `modernc.org/sqlite v1.53.0` (pure-Go). `CGO_ENABLED=0` cross-compiles work for all four targets.

**Key-dialog frontend:** `src/utils/mcpCommand.ts` already generates
```
claude mcp add --scope user --transport http <name> <url> --header "Authorization: Bearer <token>"
```
and `ApiKeySettings.vue` already displays this as the "CLI command" block in the token-reveal dialog. Tests in `src/utils/mcpCommand.test.ts` assert the exact output. No frontend code change is needed — only `docs/guides/mcp.md` must be updated.

**`mcp.md` "Local integration" section** still shows the old `cp .mcp.json.example .mcp.json` + `export DASHBOARD_MCP_TOKEN=…` approach. This must be replaced.

**`install.sh` arch mismatch (bug):** the script maps `x86_64 → x86_64`, producing `..._Darwin_x86_64.tar.gz`. GoReleaser's `.Arch` template gives `amd64`, producing `..._Darwin_amd64.tar.gz`. The download would fail for Intel targets. Fix: change the `x86_64|amd64) arch=x86_64` case to `arch=amd64`.

**`docs/guides/install.md`:** does not exist — must be created.
**`docs/RELEASING.md`:** does not exist — must be created.

**Default bind:** `127.0.0.1:13120`.

---

## Task 1 — `.goreleaser.yml` + `Dockerfile.goreleaser` + `release.yml` + `install.sh` fix

This is the riskiest task. Get `goreleaser check` clean and the snapshot building all targets + Docker before writing docs that reference the artifacts.

### Files

- `.goreleaser.yml` — add `brews`, `dockers`, `docker_manifests`; fix `snapshot` key if needed
- `Dockerfile.goreleaser` — new file (Docker build context for GoReleaser)
- `.github/workflows/release.yml` — add `packages: write`, GHCR login, buildx setup, `HOMEBREW_TAP_TOKEN` env
- `install.sh` — fix amd64 arch mapping

### Steps

**1. Fix `install.sh` arch mapping (line ~46)**

```sh
# BEFORE (wrong — produces x86_64 but goreleaser archives use amd64)
x86_64|amd64) arch=x86_64 ;;

# AFTER
x86_64|amd64) arch=amd64 ;;
```

**2. Create `Dockerfile.goreleaser`** in the repo root:

```dockerfile
FROM gcr.io/distroless/static:nonroot
COPY agent-dashboard /agent-dashboard
EXPOSE 13120
ENTRYPOINT ["/agent-dashboard", "serve"]
```

GoReleaser copies the pre-built binary named `agent-dashboard` into the Docker build context before `docker build` runs. The `nonroot` variant runs as uid 65532 (home dir `/home/nonroot`), so the `~/.claude` mount path for end users is `-v ~/.claude:/home/nonroot/.claude:ro`.

**3. Add `brews`, `dockers`, `docker_manifests` to `.goreleaser.yml`**

Append after the existing `checksum` block (before `snapshot`):

```yaml
brews:
  - name: agent-dashboard
    repository:
      owner: lx-wnk
      name: homebrew-tap
      token: "{{ .Env.HOMEBREW_TAP_TOKEN }}"
    commit_author:
      name: goreleaserbot
      email: bot@goreleaser.com
    commit_msg_template: "chore: update agent-dashboard formula to {{ .Tag }}"
    homepage: "https://github.com/lx-wnk/Agent-Dashboard"
    description: "Real-time monitoring dashboard for locally running Claude Code agents"
    license: "MIT"
    skip_upload: auto
    caveats: |
      Agent Dashboard binds to 127.0.0.1:13120 and reads ~/.claude session data.
      Never expose the dashboard port to non-loopback networks.

      Start the dashboard:
        agent-dashboard serve
      Then open http://localhost:13120
    install: |
      bin.install "agent-dashboard"
    test: |
      assert_match "agent-dashboard", shell_output("#{bin}/agent-dashboard --version")

dockers:
  - id: amd64
    goos: linux
    goarch: amd64
    image_templates:
      - "ghcr.io/lx-wnk/agent-dashboard:{{ .Version }}-amd64"
    build_flag_templates:
      - "--platform=linux/amd64"
      - "--label=org.opencontainers.image.created={{ .Date }}"
      - "--label=org.opencontainers.image.version={{ .Version }}"
      - "--label=org.opencontainers.image.source=https://github.com/lx-wnk/Agent-Dashboard"
    use: buildx
    dockerfile: Dockerfile.goreleaser
  - id: arm64
    goos: linux
    goarch: arm64
    image_templates:
      - "ghcr.io/lx-wnk/agent-dashboard:{{ .Version }}-arm64"
    build_flag_templates:
      - "--platform=linux/arm64"
      - "--label=org.opencontainers.image.created={{ .Date }}"
      - "--label=org.opencontainers.image.version={{ .Version }}"
      - "--label=org.opencontainers.image.source=https://github.com/lx-wnk/Agent-Dashboard"
    use: buildx
    dockerfile: Dockerfile.goreleaser

docker_manifests:
  - name_template: "ghcr.io/lx-wnk/agent-dashboard:{{ .Version }}"
    image_templates:
      - "ghcr.io/lx-wnk/agent-dashboard:{{ .Version }}-amd64"
      - "ghcr.io/lx-wnk/agent-dashboard:{{ .Version }}-arm64"
  - name_template: "ghcr.io/lx-wnk/agent-dashboard:latest"
    image_templates:
      - "ghcr.io/lx-wnk/agent-dashboard:{{ .Version }}-amd64"
      - "ghcr.io/lx-wnk/agent-dashboard:{{ .Version }}-arm64"
```

**4. Check `snapshot` key name**

In goreleaser v2, `snapshot.version_template` was renamed to `snapshot.name_template`. Run `goreleaser check` in step 5 — if it errors on `version_template`, rename it:

```yaml
# BEFORE
snapshot:
  version_template: '{{ incpatch .Version }}-snapshot'

# AFTER (goreleaser v2)
snapshot:
  name_template: '{{ incpatch .Version }}-snapshot'
```

**5. Update `.github/workflows/release.yml`**

Full replacement — add `packages: write`, GHCR login, buildx setup, `HOMEBREW_TAP_TOKEN`:

```yaml
name: Release

on:
  push:
    tags:
      - 'v*'

permissions:
  contents: write
  packages: write        # required for GHCR image push

jobs:
  goreleaser:
    name: GoReleaser
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
        with:
          fetch-depth: 0

      - uses: actions/setup-go@v6
        with:
          go-version: stable

      - uses: pnpm/action-setup@v6
        with:
          version: 10.33.0

      - uses: actions/setup-node@v6
        with:
          node-version: '22'
          cache: pnpm

      - name: Log in to GHCR
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      # GoReleaser's `before` hooks run `pnpm install --frozen-lockfile` + `pnpm build`,
      # emitting the SPA into server/frontend/dist so the Go build embeds it.
      - uses: goreleaser/goreleaser-action@v7
        with:
          version: ~> v2
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          HOMEBREW_TAP_TOKEN: ${{ secrets.HOMEBREW_TAP_TOKEN }}
```

### Verification

```bash
# 1. Install goreleaser if not present
goreleaser --version 2>/dev/null \
  || brew install goreleaser/tap/goreleaser \
  || go install github.com/goreleaser/goreleaser/v2@latest

# 2. Config validation (fast, no build, no Docker required)
goreleaser check
# Expected: exit 0, no errors. Fix any reported field errors before continuing.

# 3. Full dry-run (requires Docker Desktop running for docker images)
goreleaser release --snapshot --clean
# Expected:
#   - dist/ contains: agent-dashboard_*_{Darwin,Linux}_{amd64,arm64}.tar.gz
#   - dist/checksums.txt lists all four archives
#   - Docker images built locally: ghcr.io/lx-wnk/agent-dashboard:*-amd64, *-arm64
#   - No publish/push happens (snapshot mode)

# If Docker Desktop is not running, skip docker targets:
goreleaser release --snapshot --clean --skip docker
# Expected: same as above minus docker images; still validates builds and archives.
```

**Commit:**
```
git commit --no-gpg-sign -m "build: add Homebrew formula, Docker image, and multi-arch release config"
```

---

## Task 2 — `docs/guides/install.md` (new file)

### Files

- `docs/guides/install.md` — new

### Content

```markdown
# Installing Agent Dashboard

Agent Dashboard is a single self-contained binary that embeds its own web UI. No Go, Node.js, or
build tools are needed at runtime. macOS and Linux only.

**Prerequisite for all methods:** [Claude Code](https://claude.ai/code) must be installed and have
been run at least once — the dashboard reads the session data Claude Code writes to `~/.claude`.

---

## Option A — One-liner (binary, macOS / Linux)

```sh
curl -fsSL https://raw.githubusercontent.com/lx-wnk/Agent-Dashboard/main/install.sh | sh
```

Downloads the latest release archive for your platform, verifies the checksum, and installs
`agent-dashboard` to `~/.local/bin` (or `/usr/local/bin` if writable). After install:

```sh
agent-dashboard serve
```

Then open **http://localhost:13120**.

To pin a specific version or choose a different install dir:

```sh
AGENT_DASHBOARD_VERSION=v0.1.0 AGENT_DASHBOARD_BIN_DIR=~/bin \
  curl -fsSL .../install.sh | sh
```

---

## Option B — Homebrew (macOS / Linux)

```sh
brew install lx-wnk/tap/agent-dashboard
```

Requires the `lx-wnk/tap` tap to exist (it is created as part of the release process). After
install:

```sh
agent-dashboard serve
```

---

## Option C — Docker

```sh
docker run --rm \
  -p 127.0.0.1:13120:13120 \
  -v ~/.claude:/home/nonroot/.claude:ro \
  ghcr.io/lx-wnk/agent-dashboard:latest
```

Then open **http://localhost:13120**.

**Security notes:**
- The `-p 127.0.0.1:13120:13120` binding keeps the port on loopback — never use `-p 13120:13120`
  (which binds `0.0.0.0`) as the dashboard reads sensitive Claude session data.
- The `~/.claude` mount is read-only (`:ro`). The dashboard never writes to your config dir.
- If your Claude config dir differs from `~/.claude` (e.g. you have `CLAUDE_CONFIG_DIR=~/.claude-work`),
  mount that path instead and set `-e CLAUDE_CONFIG_DIR=/home/nonroot/.claude` inside the container.

**Process monitoring limitation:** Docker containers have an isolated process namespace.
The dashboard can only see processes inside the container unless you add `--pid=host`
(Linux only; requires elevated privileges). For local development monitoring, the binary or
Homebrew installs are recommended.

---

## Option D — Build from source

See [CONTRIBUTING.md](../../CONTRIBUTING.md) for prerequisites and dev setup.

```sh
git clone https://github.com/lx-wnk/Agent-Dashboard.git
cd Agent-Dashboard
pnpm build       # builds the Vue SPA into server/frontend/dist
task build       # embeds SPA → bin/agent-dashboard
./bin/agent-dashboard serve
```
```

### Steps

1. Create `docs/guides/install.md` with the content above.
2. Add an entry to the `docs/README.md` guides table:
   ```markdown
   | [Install](guides/install.md) | Binary, Homebrew, Docker, and source install paths |
   ```
   Insert this as the first row (before Configuration).

### Verification

Read through each command in the file and confirm it is runnable as written. In particular:
- The `curl | sh` one-liner works today (install.sh exists, resolves GitHub API, downloads archive — though no real release exists yet; note that this command will fail until a `v*` tag is pushed, which is intentional per D5).
- The Homebrew command will fail until the tap repo exists (D5 gate; document this in RELEASING.md).
- The Docker command will work once the image is published (D5 gate).

```bash
# Verify install.md has no broken internal links
grep '\]\(\.\./' docs/guides/install.md   # relative refs point up correctly
```

**Commit:**
```
git commit --no-gpg-sign -m "docs: add end-user install guide (binary, brew, docker, source)"
```

---

## Task 3 — README restructure + CONTRIBUTING polish

### Files

- `README.md` — restructure Quickstart to lead with end-user install; move dev prereqs to "Develop" pointer
- `CONTRIBUTING.md` — minor: confirm dev setup is comprehensive (it already is)
- `docs/README.md` — updated in Task 2

### Steps

**1. `README.md` Quickstart section — replace the current block**

Current state (lines 52–67): leads with Go/Task/air/pnpm prerequisites and `git clone`.

Replace the `## Quickstart` section with:

```markdown
## Quickstart

### Install (no build tools needed)

**One-liner (macOS / Linux):**
```sh
curl -fsSL https://raw.githubusercontent.com/lx-wnk/Agent-Dashboard/main/install.sh | sh
```

Or use Homebrew:
```sh
brew install lx-wnk/tap/agent-dashboard
```

Then:
```sh
agent-dashboard serve
```

Open **http://localhost:13120** — any running Claude Code agents appear automatically.

See [docs/guides/install.md](docs/guides/install.md) for Docker, manual binary download, and all options.

### Develop / build from source

Requires Go 1.26+, [Task](https://taskfile.dev), [air](https://github.com/air-verse/air), Node.js 22+, and [pnpm](https://pnpm.io/installation).

```sh
git clone https://github.com/lx-wnk/Agent-Dashboard.git
cd Agent-Dashboard
pnpm install
task dev        # Go backend (air hot-reload) + Vite — serves on :13120
```

When iterating on the UI, run `pnpm dev` in a second terminal for HMR on `:5173`.
See [CONTRIBUTING.md](./CONTRIBUTING.md) for the full dev setup and command reference.
```

Remove the old `### Production build` subsection from the README — it belongs in CONTRIBUTING (which already documents `task build`). Add a brief pointer if it is absent from CONTRIBUTING.

**2. `CONTRIBUTING.md` — verify production build is documented**

`CONTRIBUTING.md` already has a `## Commands` table with `task build`. The `### Production build` content from README can optionally be moved here; at minimum, confirm `task build` and the `DASHBOARD_JWT_SECRET` env var for production runs are mentioned. If missing, add a short "Production build" note to the commands section.

**3. Documentation table in README — add Install guide row**

The `## Documentation` table in README currently lists MCP, agent-control, security, architecture, statusline, agent-skills. Add:

```markdown
| [Install](docs/guides/install.md) | Download, Homebrew, Docker, build from source |
```

as the first row.

### Verification

```bash
# No TypeScript/Go changes — verify docs only
# Check no broken markdown links (manual review)
grep '\]\(docs/' README.md   # all docs/ links still valid
grep '\]\(CONTRIBUTING' README.md
```

**Commit:**
```
git commit --no-gpg-sign -m "docs: lead README quickstart with binary install; move dev prereqs to CONTRIBUTING pointer"
```

---

## Task 4 — `docs/guides/mcp.md` + frontend verification

### Files

- `docs/guides/mcp.md` — replace "Local integration" with key-dialog flow + `claude mcp add` one-liner

### Current "Local integration" section (full content, lines 32–40):

```markdown
## Local integration

Copy the shipped example and export a token — any Claude Code session opened in this repo then auto-connects to the dashboard MCP:

```bash
cp .mcp.json.example .mcp.json
export DASHBOARD_MCP_TOKEN=mcp_<your-token>
```

`.mcp.json` is gitignored to prevent accidental token commits.
```

### Replacement

Replace the entire `## Local integration` section with:

```markdown
## Connect the dashboard to Claude

The fastest way to wire a Claude Code session to the dashboard's task tools is the one-command
method available in the key dialog.

### One-command setup (recommended)

1. Open **Settings → API Keys** in the dashboard.
2. Click **+ Add Key**, choose the **Developer** or **Admin** role, and click **Create Key**.
3. In the token-reveal dialog, find the **CLI command** block — it shows a ready-to-run command
   like:
   ```sh
   claude mcp add --scope user --transport http agent-dashboard \
     http://127.0.0.1:13120/api/mcp \
     --header "Authorization: Bearer mcp_<your-token>"
   ```
4. Click the copy button, run the command in your terminal. It writes the MCP server config to
   `~/.claude.json` at user scope — every Claude Code session you open will auto-connect to the
   dashboard.

The `--scope user` flag makes the connection global (all sessions). To scope it to one project
only, replace `--scope user` with `--scope project` — this writes to the project's `.mcp.json`
instead.

> **Verify the exact `claude mcp add` flags for your Claude Code version** by running
> `claude mcp add --help`. The flags above match the HTTP transport syntax as of Claude Code
> 2025. If a flag name differs, adapt accordingly and update this doc.

### Manual / JSON config alternative

If you prefer to manage the config file directly, add an entry to your `.mcp.json` (project-scoped)
or `~/.claude.json` (user-scoped):

```json
{
  "mcpServers": {
    "agent-dashboard": {
      "type": "http",
      "url": "http://127.0.0.1:13120/api/mcp",
      "headers": {
        "Authorization": "Bearer mcp_<your-token>"
      }
    }
  }
}
```

`.mcp.json` is gitignored to prevent accidental token commits. The JSON block is also available in
the key dialog's **JSON config** block for one-click copy.
```

### Steps

1. Replace the `## Local integration` section in `docs/guides/mcp.md` with the content above.
2. Run the frontend test to confirm the `buildMcpAddCommand` utility produces the documented format:

```bash
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard
pnpm test -- mcpCommand
```

Expected: all 7 tests in `src/utils/mcpCommand.test.ts` pass. The primary assertion (line 8–11):
```
'claude mcp add --scope user --transport http dashboard-tasks '
+ 'https://dash.example.com/api/mcp '
+ '--header "Authorization: Bearer mcp_abc123"'
```
confirms the format documented in mcp.md.

3. Verify the key-dialog component renders this correctly end-to-end:
```bash
pnpm typecheck
pnpm lint
```

Both must exit 0 (no frontend code was changed; these confirm the existing code is still valid after the doc update).

### Verification

```bash
# mcpCommand tests
pnpm test -- mcpCommand
# Expected: 7 passed

# Type and lint clean
pnpm typecheck && pnpm lint
# Expected: both exit 0
```

**Commit:**
```
git commit --no-gpg-sign -m "docs: replace mcp.md local-integration with claude mcp add one-liner flow"
```

---

## Task 5 — `docs/RELEASING.md` (new file)

### Files

- `docs/RELEASING.md` — new

### Content

```markdown
# Releasing Agent Dashboard

This document is for repository maintainers. End users: see [docs/guides/install.md](guides/install.md).

## Version scheme

[Semantic Versioning](https://semver.org): `vMAJOR.MINOR.PATCH`. The first public release is `v0.1.0`.
Use a `v` prefix on all tags (`v0.1.0`, not `0.1.0`).

## One-time setup (per repo / secrets)

These steps are done once and do not repeat per release.

### 1. Create the Homebrew tap repo

Create a public GitHub repository named `homebrew-tap` under the `lx-wnk` org:
`https://github.com/lx-wnk/homebrew-tap`

GoReleaser will push the formula to this repo. It must be public for `brew install lx-wnk/tap/…` to work.

### 2. Create a `HOMEBREW_TAP_TOKEN` secret

The formula push needs a Personal Access Token (PAT) with `repo` write scope on `lx-wnk/homebrew-tap`.

1. Create a PAT at **GitHub → Settings → Developer settings → Personal access tokens (classic)**.
   Scopes: `repo` (full repo access, allows push to `homebrew-tap`).
2. Add it as a repository secret in `Agent-Dashboard`:
   **Settings → Secrets and variables → Actions → New repository secret**
   Name: `HOMEBREW_TAP_TOKEN`

### 3. Verify GHCR is enabled

`ghcr.io/lx-wnk/agent-dashboard` images are pushed using the `GITHUB_TOKEN` (no extra secret needed)
because `.github/workflows/release.yml` has `permissions.packages: write`.

After the first image is pushed, the package visibility may default to private. Set it to public at:
`https://github.com/lx-wnk?tab=packages` → `agent-dashboard` → **Package settings → Change visibility → Public**.

## Release steps (per release)

1. **Update CHANGELOG.md** — rename `## [Unreleased]` to `## [X.Y.Z] — YYYY-MM-DD`; add a new blank
   `## [Unreleased]` above it. GoReleaser will use git commit history for the GitHub release notes
   (the changelog block in `.goreleaser.yml`); the `CHANGELOG.md` file is for the human-readable record.

2. **Merge to main and push** — the release commit must be on `main`.

3. **Tag and push:**
   ```sh
   git tag v0.1.0
   git push origin v0.1.0
   ```

4. **Watch the Release workflow** at `https://github.com/lx-wnk/Agent-Dashboard/actions`.
   It runs GoReleaser which:
   - Builds four binaries (darwin/linux × amd64/arm64)
   - Creates four `.tar.gz` archives + `checksums.txt` → GitHub release assets
   - Pushes the Homebrew formula to `lx-wnk/homebrew-tap`
   - Pushes `ghcr.io/lx-wnk/agent-dashboard:{version}` and `:latest` Docker images

5. **Verify:**
   - GitHub release page shows four archives, `checksums.txt`, `install.sh` download works
   - `brew install lx-wnk/tap/agent-dashboard` succeeds (allow a few minutes for the tap to propagate)
   - `docker pull ghcr.io/lx-wnk/agent-dashboard:latest` succeeds

## Dry-run before tagging (recommended)

Run the snapshot locally before pushing a tag to catch config problems:

```sh
# Install goreleaser if not present
brew install goreleaser/tap/goreleaser

# Validate config (fast, no build)
goreleaser check

# Full dry-run — builds all targets and Docker images, no publish
goreleaser release --snapshot --clean

# Skip Docker if Docker Desktop is not running
goreleaser release --snapshot --clean --skip docker
```

The snapshot artifacts land in `dist/`. Check that all four archives and `checksums.txt` are present
before pushing the tag.

## Rollback

- Remove the tag: `git push origin :v0.1.0` and `git tag -d v0.1.0`
- Delete the GitHub release (draft or published)
- The Homebrew formula must be reverted manually in `lx-wnk/homebrew-tap`
- Docker images can be deleted from GHCR package settings

## Security reminder

`agent-dashboard serve` binds to `127.0.0.1` only. The Docker run command in all docs uses
`-p 127.0.0.1:13120:13120` — never omit the host binding.
```

### Steps

1. Create `docs/RELEASING.md` with the content above.
2. Add a reference in `docs/README.md` under a new `## Releasing` header (after the Architecture table):
   ```markdown
   ## Releasing
   See [RELEASING.md](RELEASING.md) for the version scheme, one-time setup, and per-release steps.
   ```

### Verification

Read through each command in the file and confirm it is actionable. No automated check for this task.

**Commit:**
```
git commit --no-gpg-sign -m "docs: add RELEASING.md with version scheme, one-time setup, and release steps"
```

---

## Task 6 — CHANGELOG `[Unreleased]` entry

### Files

- `CHANGELOG.md` — add a bullet under `### Added` in `## [Unreleased]`

### Steps

Add to the `### Added` list in `## [Unreleased]` (before the first existing bullet):

```markdown
- End-user install paths: binary (one-liner via `install.sh`), Homebrew (`brew install lx-wnk/tap/agent-dashboard`), and Docker (`ghcr.io/lx-wnk/agent-dashboard`) — no Go or build tools required at runtime. See [`docs/guides/install.md`](docs/guides/install.md).
- One-command MCP connect: the API key dialog's **CLI command** block now copies a `claude mcp add --scope user --transport http …` one-liner with the generated key substituted. Documented in [`docs/guides/mcp.md`](docs/guides/mcp.md#connect-the-dashboard-to-claude).
- GoReleaser config now publishes Homebrew formulae to `lx-wnk/homebrew-tap` and multi-arch Docker images (`linux/amd64`, `linux/arm64`) to GHCR on `v*` tag pushes. See [`docs/RELEASING.md`](docs/RELEASING.md).
```

### Verification

```bash
# Confirm the CHANGELOG parses as valid Markdown (no broken headings)
grep '^## ' CHANGELOG.md
# Expected: "## [Unreleased]" as first heading, followed by prior version entries
```

**Commit:**
```
git commit --no-gpg-sign -m "docs: changelog entries for publishing and MCP onboarding"
```

---

## Final Verification Pass

Run all of these in order before opening the PR:

```bash
# 1. GoReleaser config is valid
goreleaser check
# Expected: exit 0

# 2. GoReleaser snapshot (all targets + docker if available)
goreleaser release --snapshot --clean
# Expected: dist/ has four .tar.gz archives + checksums.txt; docker images built

# 3. Go build still works
cd server && go build ./...
# Expected: exit 0

# 4. Frontend SPA build (embeds into binary)
pnpm build
# Expected: exit 0, dist files in server/frontend/dist

# 5. Frontend checks
pnpm typecheck && pnpm lint
# Expected: both exit 0

# 6. Key-dialog unit test
pnpm test -- mcpCommand
# Expected: 7 tests pass

# 7. Link sanity
grep '\]\(' docs/guides/install.md | grep -v 'http'
# Verify all relative links point to files that exist
```

---

## Commit Order Summary

| # | Commit message | Files |
|---|---|---|
| 1 | `build: add Homebrew formula, Docker image, and multi-arch release config` | `.goreleaser.yml`, `Dockerfile.goreleaser`, `.github/workflows/release.yml`, `install.sh` |
| 2 | `docs: add end-user install guide (binary, brew, docker, source)` | `docs/guides/install.md`, `docs/README.md` |
| 3 | `docs: lead README quickstart with binary install; move dev prereqs to CONTRIBUTING pointer` | `README.md`, `CONTRIBUTING.md` (minor) |
| 4 | `docs: replace mcp.md local-integration with claude mcp add one-liner flow` | `docs/guides/mcp.md` |
| 5 | `docs: add RELEASING.md with version scheme, one-time setup, and release steps` | `docs/RELEASING.md`, `docs/README.md` |
| 6 | `docs: changelog entries for publishing and MCP onboarding` | `CHANGELOG.md` |

All commits: `git commit --no-gpg-sign` (SSH signing lock workaround — confirmed from project memory).

---

## Spec / Reality Mismatches & Notes

1. **`install.sh` arch bug (not in spec):** The script maps `x86_64 → x86_64` but GoReleaser's `.Arch` for Intel is `amd64`. Downloads would 404 for Intel targets on first tag. Fix is in Task 1, step 1.

2. **Frontend key-dialog already done:** The spec says "update the key-dialog copy-command (frontend component that currently copies an export/example)". In reality, `ApiKeySettings.vue` + `src/utils/mcpCommand.ts` already generate and display the `claude mcp add` command (implemented in PR #151). No frontend code change is needed — only `docs/guides/mcp.md` must be updated. The test in `mcpCommand.test.ts` already validates the format.

3. **`goreleaser check` may flag `snapshot.version_template`:** GoReleaser v2 renamed this key to `name_template`. If `goreleaser check` errors on it, rename it in place (Task 1, step 4). This is a pre-existing config quirk not introduced by this plan.

4. **Docker process monitoring limitation:** The spec does not mention it, but containers have an isolated PID namespace. The Docker install is useful for running the UI/task pipeline features, but for monitoring host `claude` processes the binary or Homebrew install is better. Documented in install.md.

5. **`claude mcp add` flag verification:** The exact flags (`--scope user --transport http … --header`) are already validated by the existing test suite (`mcpCommand.test.ts`). The implementer should still run `claude mcp add --help` to confirm they match the installed Claude Code version, and adapt if a flag name changed.

6. **`homebrew-tap` repo and `HOMEBREW_TAP_TOKEN` are one-time operator setup:** The snapshot dry-run does NOT require the tap token (snapshot skips publishing). The workflow secret is only needed at real release time. Documented in RELEASING.md.

7. **`docker_manifests` requires push to create the manifest:** In snapshot mode, manifests are built but not pushed. On the first real release, GHCR must have the per-arch images pushed before the manifest push succeeds — GoReleaser handles this ordering automatically.
