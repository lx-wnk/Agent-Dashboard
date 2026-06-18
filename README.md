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

- 🔌 **Zero-config monitoring** — discovers agents by scanning processes (`ps`/`lsof`); no per-project hooks or wrappers
- 🔁 **Autonomous task pipeline** — a real state machine that spawns a `claude` CLI process per stage in an isolated git worktree
- 🎛️ **MCP control plane** — an authenticated MCP endpoint with scoped tokens lets agents (and you) drive the dashboard back
- 🔐 **Auth & permissions** — GitHub OAuth + JWT, scoped API keys, and a two-way permission gate for spawned agents
- 🏠 **Local-first** — binds to `127.0.0.1`, hashes tokens, makes no outbound calls unless you opt in. No telemetry, no SaaS

> 📄 [Privacy policy](./PRIVACY.md) · 🔒 [Security model](docs/guides/security.md)

## Features

**Monitoring**
- Real-time agent view over SSE (list, card, and kanban layouts) with status, model, tokens, cost, and uptime
- Chat-style session transcript with markdown rendering, collapsible tool groups, inline task checklists, and subagent badges
- FTS5 spotlight search (`Cmd+K`) across tasks and agents
- N-gram pattern discovery surfacing common tool-use sequences
- Historical session cost import with streaming progress

**Control**
- Send follow-up instructions and `/btw` interrupts to running agents via MCP channels
- Spawn new agents with custom prompts, models, and system prompts from the UI
- Multi-stage task pipeline (`concept → implementation → review → done`) with per-stage permission gates and parallel execution
- Refinement chat to shape a concept before it enters implementation
- Authenticated MCP endpoint (19 tools, 4 scopes) + scoped API key management

**Integrations**
- Web Push, webhook, email, and system notifications with per-event routing
- Pluggable LLM adapters and sidecar plugins
- Remote registrations to aggregate agents across machines
- In-dashboard `~/.claude` config explorer — browse, edit, and save skills, slash commands, and memory files
- Git worktree panel — create and remove worktrees for pipeline tasks directly from the UI
- Frontend plugin slot framework — mount custom UI into named extension points (refinement, settings, and more)
- Memory browser for Claude agent memory files
- Shell statusline (`scripts/statusline.py`), dark/light theme, PWA support

## Quickstart

**Prerequisites:** macOS or Linux, [Go 1.26+](https://go.dev/), [Task](https://taskfile.dev) (`brew install go-task/tap/go-task`), [air](https://github.com/air-verse/air) (`go install github.com/air-verse/air@latest`), Node.js 22+ with [pnpm](https://pnpm.io/installation), and Claude Code itself.

> `go install` binaries land in `$(go env GOPATH)/bin` — make sure it's on your `PATH`:
> `echo 'export PATH="$PATH:$(go env GOPATH)/bin"' >> ~/.zshrc && source ~/.zshrc`

```bash
git clone https://github.com/lx-wnk/Agent-Dashboard.git
cd Agent-Dashboard
pnpm install        # frontend dependencies (Go deps are fetched on first build)
task dev            # Go backend (air hot-reload) + Vite — serves on :13120
```

Open **http://localhost:13120** — any running Claude Code agents appear automatically. On loopback with no OAuth configured, the dashboard runs in local-trust mode (no login). See [Security](docs/guides/security.md) before exposing it anywhere.

When iterating on the UI, run `pnpm dev` in a second terminal for HMR on `:5173` (it proxies `/api` → `:13120`).

### Production build

```bash
pnpm build          # build the Vue SPA (embedded into the binary via go:embed)
task build          # compile → bin/agent-dashboard
DASHBOARD_JWT_SECRET=<32+ chars> ./bin/agent-dashboard serve
```

Build the SPA **before** the binary — `go:embed` bakes the compiled frontend into `bin/agent-dashboard`. The result is self-contained: no Node.js needed at runtime. Deploy by copying the binary.

## Documentation

| Topic | |
|---|---|
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
