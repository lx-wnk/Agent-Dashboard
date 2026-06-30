# Documentation

Reference and guides for Agent Dashboard. Start with the [project README](../README.md) for an overview and quickstart.

## Guides

| Guide | What it covers |
|---|---|
| [Install](guides/install.md) | Binary, Homebrew, Docker, and source install paths |
| [Configuration](guides/configuration.md) | Every environment variable, grouped by concern |
| [MCP Endpoint](guides/mcp.md) | Authenticated MCP server, scopes, the tools, and connecting Claude |
| [Controlling & Spawning Agents](guides/agent-control.md) | Channel control, the spawn dialog, slash commands, permissions |
| [Security](guides/security.md) | Threat model, auth, hardening defaults |
| [Shell Statusline](guides/statusline.md) | `scripts/statusline.py` PS1 integration |
| [Agent Skills](guides/agent-skills.md) | Installing the project's AI agent skills |
| [Plugins](plugin-guide.md) | Sidecar plugin architecture |
| [Hooks setup](hooks-setup.md) | Optional hook-based rescan triggers |

## Architecture

| Document | What it covers |
|---|---|
| [Overview](architecture/overview.md) | Stack, package layout, data flow, task pipeline |
| [ADR-0001](architecture/adr/0001-sqlite-for-task-pipeline.md) | Why SQLite backs the task pipeline |
| [ADR-0002](architecture/adr/0002-runner-slot-priority-model.md) | Runner-slot priority model |
| [ADR-0003](architecture/adr/0003-pluggable-spawners.md) | Pluggable spawner adapters |

## Contributing

See [CONTRIBUTING.md](../CONTRIBUTING.md) for setup, commands, PR process, and code guidelines.
