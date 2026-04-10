# AGENTS.md — Project Bootstrap

> All agents MUST read and follow this file.

## Identity

**claude-agent-overview** | Vue 3 + Express 5 + TypeScript | No Docker (native Node)

## Shared Configuration

@.agent-context/agent-startup.md

## Context Architecture

| Layer | File                                     | Content                         |
| ----- | ---------------------------------------- | ------------------------------- |
| 0     | @.agent-context/layer0-agent-workflow.md | Agent Workflow (shared)         |
| 1     | @.agent-context/layer1-bootstrap.md      | Project identity, tech stack    |
| 2     | @.agent-context/layer2-project-core.md   | Dev principles + critical rules |
| 3     | @.agent-context/layer3-guidebook.md      | Task routing, skills, memory    |

## Quick Rules (Always Apply)

- Server binds to `127.0.0.1` only — never expose to network (reads sensitive session data)
- macOS only — process scanning uses `ps`/`lsof`, system monitor uses macOS-specific `top` flags
- No database — all data from Claude Code's filesystem + running processes
- Single dev command: `npm run dev` (Express + Vite on port 13120)

## Compaction Preservation

When compacting context, always preserve:

- List of modified/created files in this session
- Active test/lint commands and their last results
- Unfinished tasks and next steps
