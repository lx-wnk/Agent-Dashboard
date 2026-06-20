# Layer 3 — Guidebook

> When to load what. This is the single reference for navigating the knowledge base.

## Before You Start Any Task

1. Read `memory/lessons.md` — avoid repeating past mistakes
2. Read `memory/todo.md` — check if there's an active task plan
3. If you're unsure which memory files exist or are relevant: read `memory/index.md`

Skip files that are empty or contain only comments.

## Load By Task Type

These files are **load-on-demand** (not eager-imported via `AGENTS.md`). `Read` the matching one when your task enters that area:

| Working on...                       | Read first                                          |
| ----------------------------------- | --------------------------------------------------- |
| Backend / API                       | `server/` source, `decisions.json`                  |
| Frontend / Components               | `src/` source, `decisions.json`                     |
| Process scanning                    | `server/processScanner.ts`                          |
| JSONL parsing                       | `server/jsonlParser.ts`                             |
| Modules / data flow overview        | `.agent-context/architecture.md`                    |
| Pipeline state machine / sweeps / Go layering | `.agent-context/task-pipeline.md`         |
| MCP endpoint / auth / scopes        | `.agent-context/mcp.md`                             |
| Pipeline permissions / grants       | `.agent-context/permissions.md`                     |

## Skills Index

@.agent-context/skills/index.md

## Memory Files

| File                | Purpose                    | Load           |
| ------------------- | -------------------------- | -------------- |
| `memory/lessons.md` | Hard-won lessons           | Session start  |
| `memory/todo.md`    | Current task plan          | Session start  |
| `memory/index.md`   | Memory file catalog        | When uncertain |
| `memory/log.md`     | Chronological activity log | On-demand      |
| `decisions.json`    | Architectural decisions    | On-demand      |
| `memory/people.md`  | Team & stakeholders        | On-demand      |

## After You Finish

> Update immediately — don't defer to a future session. See Layer 0 for full rules.

1. User correction or self-discovered insight? → `memory/lessons.md`
2. Architecture/design decision made? → `decisions.json`
3. Task complete? → Update `memory/todo.md`
4. User stated preference? → `memory/preferences.md`
5. Learned about user? → `memory/user.md`
6. Learned about team? → `memory/people.md`
7. Significant decision or event this session? → append to `memory/log.md`
