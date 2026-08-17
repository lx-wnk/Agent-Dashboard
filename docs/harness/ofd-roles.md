# OFD Sub-Subagent Role Contracts

All sub-subagents: operate ONLY in the worktree path the orchestrator gives you. Never pass `isolation` to any Agent call. Never edit outside the worktree.

## Implementer
- `subagent_type`: `agents:frontend` (Vue/TS) or `agents:backend` (Go), chosen per task.
- Input: one plan task's steps, the worktree absolute path.
- Method: TDD — write the failing test first, run it to confirm it fails, then write the minimal code to pass, then run tests green. Follow existing codebase patterns. Default to NO comments (project rule).
- Output (return message): list of files changed, the exact test command run, and its pass/fail tail. Do not commit — the orchestrator commits.

## Reviewer
- `subagent_type`: `agents:review` (read-only).
- Input: the task diff (`git -C <worktree> diff <range>`), the task's intent.
- Method: review for correctness bugs, project-convention violations (DRY, single-responsibility, no stray comments, SSOT), and missing tests. Do not edit code.
- Output: severity-tagged findings (`blocking` | `nit`). If no blocking findings, return exactly `REVIEW_CLEAN`.

## Verifier
- `subagent_type`: `general-purpose`.
- Input: the worktree absolute path.
- Method: run `pnpm lint && pnpm typecheck && pnpm test` then `go build ./... && go test ./...` (server + sdk as touched). Trust in-worktree `go build` over any LSP diagnostics (worktrees emit false gopls errors).
- Output: `VERIFY_GREEN` or `VERIFY_RED` followed by the verbatim failing output tail.
