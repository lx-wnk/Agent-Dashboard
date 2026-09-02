<div align="center">

# Agent Dashboard

**A real-time monitoring and control plane for locally running Claude Code agents.**

See every running agent at a glance — tokens, cost, status, tools, tasks, and subagents — then send instructions, spawn new agents, and drive a multi-stage task pipeline, all from a local browser tab.

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](./LICENSE)
[![Go 1.26](https://img.shields.io/badge/go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Vue 3](https://img.shields.io/badge/vue-3-4FC08D?logo=vue.js&logoColor=white)](https://vuejs.org/)
[![pnpm](https://img.shields.io/badge/pnpm-10-F69220?logo=pnpm&logoColor=white)](https://pnpm.io/)
[![Status](https://img.shields.io/badge/status-active-brightgreen)](https://github.com/lx-wnk/Agent-Dashboard)

</div>

<p align="center">
  <img src="docs/assets/hero.png" alt="Agent Dashboard — live agent roster with permission triage band, per-agent cost, and system footer" width="900">
</p>

## Why Agent Dashboard

Most agent monitors require you to wire hooks or wrappers into every project. This one doesn't — it reads what Claude Code already writes to disk and watches the processes you already run.

- 🔌 **Zero-config monitoring** — discovers agents by scanning processes (`ps`/`lsof`) and reading `CLAUDE_CONFIG_DIR` from them to tell profiles apart; no per-project hooks or wrappers
- 🔁 **Autonomous task pipeline** — a real state machine that spawns a `claude` CLI process per stage in an isolated git worktree
- 🎛️ **MCP control plane** — an authenticated MCP endpoint with scoped tokens lets agents (and you) drive the dashboard back
- 🔐 **Auth & permissions** — GitHub OAuth + JWT, scoped API keys, and a capability gate: one shared decision model (allow/deny/ask, ranked across six context levels) that resolves every spawned task-pipeline agent's permissions (still only the task level for spawns) and, separately, every memory read/write (up to two levels: a project/application scope plus the global fallback) — see [Security model](docs/guides/security.md) for which of its three enforcement points are actually wired in and which fails open
- 🙋 **Answer prompts where you are** — opt-in hooks let you approve or deny a session's permission prompt from the dashboard instead of its terminal, for any session including ones you started by hand; when nobody answers, it falls back to the terminal prompt untouched
- 🏠 **Local-first** — binds to `127.0.0.1`, hashes tokens, makes no outbound calls unless you opt in. No telemetry, no SaaS

> 📄 [Privacy policy](./PRIVACY.md) · 🔒 [Security model](docs/guides/security.md)

## Features

**Monitor** — real-time agent roster over SSE (list, card, kanban) with tokens, cost, status, and uptime; live active-subtask metrics (token usage, duration, latest output) on every agent and task card; chat-style transcript with collapsible tool groups and subagent badges; `Cmd+K` spotlight search and n-gram pattern discovery.

A **Working** indicator shows when an agent is actively generating, rather than just recently active. It's inferred from whether the agent owes the next reply (conversation turn-state) together with live session output from tmux or the pty broker, so it's distinct from the staleness-based active/waiting/idle states.

When a controllable (channel/MCP-connected) agent exits, its card stays on the dashboard as a **Finished** card in the card grid view instead of vanishing — click it to view the final result, or click ✕ to dismiss it (which removes the agent's channel discovery file). Finished cards are tracked per server run, so after a dashboard restart they're gone; reach those sessions through the **Sessions** overview instead. Agents started in a plain terminal without the dashboard channel still disappear when they exit.

**Build & control** — multi-stage task pipeline (`concept → implementation → review → done`) running in isolated git worktrees behind per-stage permission gates; per-stage engine and model selection (global and per-project), plus a per-stage reasoning-effort setting for the `claude` adapter (`low` through `max`, reaching the spawned CLI process as `--effort`) — always shown, disabled with a reason for adapters that don't support it; spawn agents and send follow-ups or `/btw` interrupts via MCP channels; refinement chat to shape a concept first; an opt-in plan-review stage that auto-generates and self-reviews the implementation plan and gates on your approval before any files are edited; seed a task straight from a GitHub or Jira issue — paste a reference into the New-Task form's **Import from issue** field to pre-fill the title and description (tracker tokens are stored encrypted and managed under **Settings → Tracker**); shared scratchpads and lease-based locks for agent coordination (`agent:coord` MCP scope); per-turn worktree checkpoints with one-click revert (a debounced filesystem watcher snapshots each agent turn into a hidden git ref; revert restores the worktree and parks the task for manual resume); drag-and-drop task reordering.

Agents spawned from the dashboard run as interactive **live** sessions rather than one-shot runs, so you can converse with them as they work. Live injection uses tmux when it's available (the agent runs in a detached session) and falls back to a built-in pty broker otherwise. The spawned agent's chat modal opens automatically once it appears on the roster.

A live-injectable session's agent modal has a **Terminal** tab — a real `xterm.js` terminal streamed over a WebSocket (`GET /api/agents/{pid}/terminal`), so you see the session's actual pty output and can type into it directly. When the session asks an interactive multiple-choice question (`AskUserQuestion`), an overlay detects it from the live terminal screen and renders the options inline — pick them and **Send answer** drives the session's selector with real keystrokes over the same WebSocket. A multi-question flow's closing **review/submit** screen ("Ready to submit your answers?") is detected the same way, so the whole round can be completed without touching the terminal. Because it drives the pty directly, this works over any transport (tmux or the built-in pty broker). The same cards appear in the main needs-you band, backed by server-side detection of the session's rendered screen.

**Extend** — authenticated MCP control plane with scoped tokens; in-dashboard `~/.claude` config explorer and git-worktree panel; frontend plugin slots and pluggable LLM adapters (OpenAI-compatible, Ollama, `anthropic`); Web Push, webhook, and email notifications. Plugins are enabled and disabled **live** from **Settings → Plugins** via `POST /api/plugins/{id}/activate` and `/deactivate` — no server restart needed for most plugins. Exception: plugins with the `auth_provider` capability are boot-wired (they affect server startup) and require a restart to take effect — after toggling one, the panel shows a "Restart required to apply" badge and a **Restart server** button; clicking it triggers the restart and a reconnect overlay polls until the server is back, then reloads the page automatically. Per-plugin settings with schema-defined fields (string, URL, integer, boolean, enum) are editable in the same panel; secret fields are masked and unchanged secrets are preserved on save. Route and UI extension plugins are reverse-proxied through a single catch-all at `/api/plugins/{id}/proxy/*`; a stopped or crashed plugin returns HTTP 503 rather than silently disappearing. A system-owned **memory** store lets agents and you persist facts, preferences, and lessons that outlive one task (`memory_search`/`memory_write` MCP tools, `/api/memory/*` HTTP routes, both gated by the capability model above) — a budgeted, ranked extract is automatically appended to every stage's spawn prompt. **Settings → Registry** lists the registry's own rows — applications, routines, skills and memory spaces — by kind and scope, over a read-only `GET /api/resources`; the memory-spaces kind needs a `memory.read` grant in the scope being viewed, the same one `/api/memory/spaces` requires. Routines are the scheduled tasks, projected from `task_schedule` rather than mirrored into the registry table, and the id each row reports is the one a `--scope routine:<id>` grant names — today that grant decides the automatic memory push into the tasks that routine fires, and nothing else (see [Security](docs/guides/security.md#capabilities-and-the-permission-gate)). **Settings → Memory** browses the spaces in a scope, opens any one of them to list its entries, searches entries by text, and creates, supersedes and expires them — a space has to be openable because an empty search query deliberately returns nothing, so search alone could never show what a space holds; the task modal's **Stages** tab shows what each stage run's spawn actually received — entries pushed, characters spent against the budget, and how many candidates the ranker chose from — read from `GET /api/memory/injections`, which gates on `memory.read` at global scope. See [Privacy policy](./PRIVACY.md) for what it stores and how it's removed. An Obsidian vault is registered as a resource-registry **Application** (`server/internal/apps/obsidian`) with four gated capabilities (read/search/write/delete): configure it from **Settings → Obsidian**, then reach it through `POST /api/obsidian/index` (turns vault notes into memory pointers) or four capability-gated `obsidian_*` MCP tools that read, search, write, and delete notes directly — see [Obsidian vault](#obsidian-vault) below for what it takes to actually turn it on.

**Supported agents / providers** — Claude Code is monitored by default. Codex CLI, Gemini CLI, and Junie CLI can be monitored too, but are opt-in. Enable or disable them from **Settings → Providers** in the dashboard UI — the change is persisted and takes effect within a few seconds without a restart. You can also set `DASHBOARD_PROVIDERS_ENABLED` to a comma-separated list of ids (`codex`, `gemini`, `junie`) as a fallback when no database row exists, or drop your own descriptor YAML files in a directory pointed to by `DASHBOARD_PROVIDER_DIR`. Agents using a local Ollama-served model show a cost of **$0** ("local") instead of "unknown".

See [`docs/`](docs/README.md) for the full feature reference.

## Quickstart

**Prerequisite:** [Claude Code](https://claude.ai/code) installed and run at least once — the dashboard reads the session data it writes to `~/.claude`. macOS and Linux only.

### Install (no build tools needed)

**One-liner (macOS / Linux):**
```sh
curl -fsSL https://raw.githubusercontent.com/lx-wnk/Agent-Dashboard/main/install.sh | sh
```

Or use Homebrew (macOS):
```sh
brew install lx-wnk/tap/agent-dashboard
```

Then:
```sh
agent-dashboard serve
```

Open **http://localhost:13120** — any running Claude Code agents appear automatically. On loopback with no OAuth configured, the dashboard runs in local-trust mode (no login). See [Security](docs/guides/security.md) before exposing it anywhere.

See [docs/guides/install.md](docs/guides/install.md) for Docker, manual binary download, and all options.

### First-run setup

On first launch, a guided setup flow opens automatically and walks you through the fastest path from install to a controllable session: (1) detects the Claude Code CLI and shows its version, or the install command if it's missing; (2) connects the dashboard to Claude with one click by registering it as an MCP server in your Claude config (`claude mcp add --scope user …`), with a copy-the-command fallback if that fails; (3) discovers your existing Claude sessions and lets you make one controllable with one click, so it becomes answerable from the dashboard. Skip it and it won't show again on its own. You can re-open the same guided flow at any time from Settings → API Keys via the "Re-run first-run setup" button.

### Develop / build from source

Requires Go 1.26+, [Task](https://taskfile.dev), [air](https://github.com/air-verse/air), Node.js 22+, and [pnpm](https://pnpm.io/installation).

```bash
git clone https://github.com/lx-wnk/Agent-Dashboard.git
cd Agent-Dashboard
pnpm install        # frontend dependencies (Go deps are fetched on first build)
task dev            # Go backend (air hot-reload) + Vite — serves on :13120
```

When iterating on the UI, run `pnpm dev` in a second terminal for HMR on `:5173` (it proxies `/api` → `:13120`). See [CONTRIBUTING.md](./CONTRIBUTING.md) for the full dev setup, the production-build steps, and the command reference.

## Desktop app (macOS)

A native macOS shell (`desktop/`, [wails](https://wails.io) v2) wraps the same dashboard as one binary — no separate server process, no sidecar. It starts the dashboard HTTP server in-process on `127.0.0.1:13120` and opens a native WKWebView window pointed at it, so it's the identical Vue SPA you get in a browser tab, just packaged as an app. Other platforms keep running `agent-dashboard serve` in a browser; the desktop shell is macOS-only.

Build and run it for a smoke test (requires macOS + Xcode command-line tools; no `wails` CLI needed for this):

```bash
task desktop:run
```

That builds the SPA, embeds it, and links the shell with the wails production tag and the `UniformTypeIdentifiers` framework (a plain `go build` omits both — see [CONTRIBUTING.md](./CONTRIBUTING.md#desktop-shell-macos)), then launches the window.

An unsigned `.app`/`.dmg` build (`task desktop:dist` / `task desktop:dmg`) plus the full signing and
notarization steps are documented in [docs/desktop-distribution.md](docs/desktop-distribution.md).

**Manual smoke checklist** (real Mac):

1. The webview loads the dashboard (not a blank page) — it fetches the SPA and `/api/*` from `http://127.0.0.1:13120`, so the redirect landed on the loopback origin.
2. A mutating action (spawn an agent, create a task, answer a question) succeeds with no `403`.
3. No App Transport Security block appears in Console.app.

## Restart

`POST /api/admin/restart` triggers a validated, graceful restart. The endpoint refuses with **409** if an active `auth_provider` plugin is currently unhealthy — restarting in that state would cause an auth lockout on the next boot.

**Activating an `auth_provider` plugin requires a restart to apply** (auth is boot-wired, not live-reloadable).

**Default (`DASHBOARD_RESTART_MODE=reexec`):** the process re-execs itself in place — same PID, no supervisor needed. Works with plain `./bin/agent-dashboard serve`.

**Supervised (`DASHBOARD_RESTART_MODE=exit`):** the process exits cleanly so the supervisor relaunches it.

| Supervisor | Required config |
|---|---|
| systemd | `Restart=always` in the service unit |
| launchd | `KeepAlive` in the plist |
| Wrapper loop | `while true; do ./bin/agent-dashboard serve; done` |

## Locked out?

A bad `auth.mode` or a broken `auth_provider` plugin can lock you out of the UI. The CLI edits the SQLite database directly, so it works even while the server is down:

```bash
agent-dashboard settings set auth.mode none   # reset auth, then restart the server
agent-dashboard grants add memory.read --pattern '*' --scope global --mode allow
```

See [Configuration](docs/guides/configuration.md) for the full settings/grants/plugins CLI reference.

## Obsidian vault

Point the dashboard at a local [Obsidian](https://obsidian.md) vault, via the
[Local REST API](https://github.com/coddingtonbear/obsidian-local-rest-api) community plugin, from
**Settings → Obsidian**: base URL, vault root, API key, and TLS mode. All four settings apply only
after a server restart, and `baseURL`/`vaultRoot`/`apiKey` are a required trio: set all three, or
none. With only one or two set, the next start **fails** and names the missing keys, rather than
booting with the vault silently disabled — a vault you configured and that quietly does not run is
worse than a refused start. Clearing all three in the panel (the API key field included) turns the
integration back off.

Once configured, **Index now** in the same panel (or `POST /api/obsidian/index`) turns vault notes
into searchable memory pointers — but only once the `obsidian.read`, `obsidian.search`, and
`memory.write` capability grants exist. A fresh install denies the run otherwise:

```bash
agent-dashboard grants add obsidian.read --pattern '*' --scope global --mode allow
agent-dashboard grants add obsidian.search --pattern '*' --scope global --mode allow
agent-dashboard grants add memory.write --pattern '*' --scope global --mode allow
```

(or the same three from **Settings → Grants**). Agents can also reach the vault directly — read,
search, write, and delete a note — through four MCP tools gated the same way; see
[MCP endpoint](docs/guides/mcp.md#scopes) for the scopes and
[Security](docs/guides/security.md#obsidians-tls-trust-model) for the TLS trust model.

## Documentation

| Topic | |
|---|---|
| [Install](docs/guides/install.md) | Download, Homebrew, Docker, build from source |
| [Configuration](docs/guides/configuration.md) | Every environment variable |
| [MCP endpoint](docs/guides/mcp.md) | Scopes, tools, local integration |
| [Controlling & spawning agents](docs/guides/agent-control.md) | Channels, spawn dialog, permissions |
| [Security](docs/guides/security.md) | Threat model & hardening |
| [Architecture](docs/architecture/overview.md) | Stack, packages, data flow, pipeline |
| [Shell statusline](docs/guides/statusline.md) | PS1 integration |
| [Agent skills](docs/guides/agent-skills.md) | Installing project skills |

Full index: [`docs/`](docs/README.md).

## Contributing

Contributions are welcome. See [CONTRIBUTING.md](./CONTRIBUTING.md) for setup, the full command reference, the PR process, and code guidelines. In short:

```bash
task test           # all Go tests, race detector
task lint           # golangci-lint
pnpm typecheck      # vue-tsc
```

Found a bug or have an idea? [Open an issue](https://github.com/lx-wnk/Agent-Dashboard/issues/new/choose).

## License

[MIT](./LICENSE) © Alexander Wink
