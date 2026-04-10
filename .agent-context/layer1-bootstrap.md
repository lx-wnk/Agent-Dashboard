# Layer 1 — Bootstrap

> Project identity, architecture rules, and agent coordination.

## Identity

**claude-agent-overview** — Real-time monitoring dashboard for locally running Claude Code agents. Reads JSONL session logs and process metadata to display token usage, costs, tool activity, tasks, and subagents.

**Stack:** Vue 3, Express 5, TypeScript, Vite 6

## Platform

macOS only. `server/systemMonitor.ts` uses macOS-specific `top` flags; process scanning relies on `ps` and `lsof`.

## Data Flow

Browser polls `/api/agents` → Express scans processes (`ps`/`lsof`) → matches PIDs to `~/.claude/projects/{encoded_path}/{sessionId}.jsonl` → tail-reads JSONL + reads session-meta JSON → merges, calculates cost/status → returns `Agent[]`.

## Excluded Directories

- `node_modules/`
- `dist/`
