# Layer 1 — Bootstrap

> Project identity, architecture rules, and agent coordination.

## Identity

**claude-agent-overview** — Real-time monitoring dashboard for locally running Claude Code agents. Reads JSONL session logs and process metadata to display token usage, costs, tool activity, tasks, and subagents.

**Stack:** Vue 3, Express 5, TypeScript, Vite 6

## Platform

macOS and Linux. `server/systemMonitor.ts` uses `top` on macOS and `/proc/stat` on Linux for CPU; `server/processScanner.ts` uses `lsof` on macOS and `/proc/<pid>/cwd` on Linux. Windows is unsupported.

## Data Flow

Browser polls `/api/agents` → Express scans processes (`ps`/`lsof`) → matches PIDs to `~/.claude/projects/{encoded_path}/{sessionId}.jsonl` → tail-reads JSONL + reads session-meta JSON → merges, calculates cost/status → returns `Agent[]`.

## Excluded Directories

- `node_modules/`
- `dist/`
