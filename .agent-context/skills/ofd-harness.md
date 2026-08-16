---
name: ofd-harness
trigger: when executing an approved feature plan autonomously via an orchestrator subagent in an isolated worktree; keywords "OFD", "orchestrated delivery", "run the plan with subagents", "spawn orchestrator"
---

# OFD Harness

Reusable process: human-gated spec/plan, then **the main thread orchestrates** (default, via subagent-driven-development) — dispatching implementer/reviewer/verifier **sub-subagents** synchronously in ONE git worktree until a PR. A spawned-orchestrator background mode exists but may stall (see runbook).

- Runbook (main thread): `docs/harness/ofd-runbook.md`
- Orchestrator prompt: `docs/harness/ofd-orchestrator-prompt.md`
- Role contracts: `docs/harness/ofd-roles.md`
- Design: `docs/archive/specs/2026-06-23-ofd-harness-design.md`

Hard rule: isolation comes from the manually-created worktree (branched off `main`); never use the Agent `isolation:'worktree'` flag.
