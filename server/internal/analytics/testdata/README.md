# Analytics fixtures

Minimal JSONL session fixtures used by `visualizations_test.go` and `scan_test.go`.

| File | Purpose |
|---|---|
| `single-session-linear.jsonl` | One session, three tool calls in order: `Read → Edit → Bash`. Used for: BuildSankey (3 nodes, 2 links of value 1); BuildDAG (3 tool + 3 user tool_result nodes, chrono + result edges); BuildCoOccurrence diagonal (Read=1, Edit=1, Bash=1). |
| `two-sessions-branching.jsonl` | Session A: `Read → Edit → Bash`. Pairs with `two-sessions-branching-2.jsonl` (Session B: `Read → Grep → Bash`). Used for: BuildCoOccurrence symmetric matrix where Read+Bash co-occur in 2 sessions, Edit+Bash in 1, Grep+Bash in 1. |
| `two-sessions-branching-2.jsonl` | Session B for the branching pair. |
| `spawn-tree/` | Parent session + two subagent JSONL files (created by tests at runtime via t.TempDir to avoid committing nested fixture trees). |

Fixture timestamps are in 2026 to stay future-proof; analytics tests pass explicit `ScanOpts.From/To` bounds so absolute times do not matter.
