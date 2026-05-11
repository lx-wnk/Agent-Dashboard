---
name: Go Workspace Initialization
description: Task 1 of Go rebuild — workspace bootstrap with sdk and server modules
type: project
---

**Task:** Go Rebuild Phase 1 — Initialize Go workspace

**Completed (2026-05-10):**

- Created `go.work` at repo root with `go 1.26.3` and modules `./sdk`, `./server`
- Initialized `sdk/` module (github.com/lx-wnk/agent-dashboard/sdk)
- Initialized `server/` module (github.com/lx-wnk/agent-dashboard/server)
- Added server dependencies:
  - `github.com/go-chi/chi/v5@v5.2.5` (HTTP router)
  - `github.com/spf13/cobra@v1.10.2` (CLI framework)
  - `github.com/knadh/koanf/v2@v2.3.4` (config management)
  - `github.com/stretchr/testify@v1.11.1` (testing utilities)
  - `entgo.io/ent@v0.14.6` (ORM)
  - `github.com/google/wire@v0.7.0` (dependency injection)
  - `golang.org/x/sync@v0.20.0` (concurrency utilities)

**Commit:** `a603517` — init: initialize Go workspace with sdk and server modules

**Note on koanf:** The v2 sub-packages (providers/*, parsers/*) remain at v1 versions. This is expected; koanf v2 is the main module, sub-packages use v1 versions.

**Next steps:**
- Task 2: Scaffold core Go server structure (main.go, server package)
- Task 3: Implement process scanner and JSONL parser (Go equivalents)
