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
  sh -c "$(curl -fsSL https://raw.githubusercontent.com/lx-wnk/Agent-Dashboard/main/install.sh)"
```

---

## Option B — Homebrew (macOS)

```sh
brew install lx-wnk/tap/agent-dashboard
```

Installs the `agent-dashboard` cask from the `lx-wnk/tap` tap (created as part of the release
process). Homebrew Cask is macOS-only — on Linux use Option A (binary) or Option C (Docker)
instead. After install:

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
- The image runs as the non-root `nonroot` user (home `/home/nonroot`), so the mount target is
  `/home/nonroot/.claude`.
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
pnpm install     # frontend dependencies
pnpm build       # builds the Vue SPA into server/frontend/dist
task build       # embeds SPA → bin/agent-dashboard
./bin/agent-dashboard serve
```
