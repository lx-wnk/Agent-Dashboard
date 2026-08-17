# Plan: Go dependency-direction, leaf extraction & ADR backfill

**Goal:** Restore the intended Go layer direction in the backend by removing the one live
upward import (`db/rawrepo → internal/pipeline`), consolidating two duplicated Ollama HTTP
clients into a transport-only leaf, replacing the fragile 12-value nil-padded server
constructor with a named struct, and adding a cross-language SSOT drift guard for the task-slug
regex. The layering invariant is then enforced in CI via `depguard`, and four backfill ADRs
record the leaf pattern, the local-first single-process boundary, the SSOT-parity policy, and
the plugin domain boundaries.

**Architecture:**
- `server/internal/proc/` (new leaf) — stdlib-only process-liveness probe (`IsPidAlive`,
  `isPidZombie`), moved out of `pipeline/session_manager.go`. Depends only on
  `os`, `os/exec`, `syscall`, `strings`. Importable by any layer.
- `server/internal/ollamaclient/` (new leaf) — transport-only Ollama HTTP client
  (host normalization, `POST /api/chat`, `GET /api/tags`). Two existing callers keep their
  distinct behaviour (spawner vs classifier) but share the wire layer.
- `server/serverapp/di.go` — `initializeServer` return signature collapses from 12 positional
  values to `(*ServerComponents, error)`.
- `server/internal/validation/slug.go` (unchanged) + `src/utils/validation.ts` (unchanged) —
  new Vitest parity test asserts the two `SLUG_RE` literals stay byte-identical.
- `.golangci.yml` — new `depguard` rule: `db/rawrepo` and `api/**` may not import
  `internal/pipeline`.
- `docs/architecture/adr/0009..0012` (new) + `0004` (amended).

**Tech Stack:** Go 1.26 (chi, ent, modernc/sqlite, cobra) · Vue 3 TS + Vite + Vitest ·
golangci-lint v2 (`depguard`) · Taskfile (`task test`, `task build`).

**Sequencing rationale:** Task 1 removes the invariant violation and lands the lint that
prevents regressions; Tasks 2–4 are independent and can be parallelized after Task 1; Task 5 is
a docs-only amendment. Tasks 1, 3, 4 are one mechanical PR each; Task 2 carries a genuine
design decision (the shared seam); Task 5 is XS.

---

## Task 1 — Extract `internal/proc` liveness leaf + enforce boundary

**Type:** mechanical move + re-import, plus one design-light lint rule.
**Why:** `db/rawrepo/stage_run_bulk_repo.go:11,53` imports `internal/pipeline` solely for
`pipeline.IsPidAlive`, and `api/tasks/{enrich,analyze_routes}.go` do the same. Infra and edge
layers importing the orchestration core inverts the layer direction and seeds a latent
`rawrepo ↔ pipeline` cycle. `IsPidAlive`/`isPidZombie` are stdlib-only and reference nothing in
the pipeline state machine — the textbook leaf-extraction case (precedent: ADR-0005 llmadapter,
ADR-0006 worktree).

### Files
- `server/internal/proc/proc.go` (new) — `package proc`; `IsPidAlive(pid int) bool` +
  unexported `isPidZombie(pid int) bool` moved verbatim from
  `server/internal/pipeline/session_manager.go:34`.
- `server/internal/proc/proc_test.go` (new) — move the liveness cases from
  `pipeline/session_manager_test.go`; add a self-pid-alive + pid<=0 + bogus-pid case.
- `server/internal/pipeline/session_manager.go` — delete the moved functions.
- Importers to migrate (`pipeline.IsPidAlive` → `proc.IsPidAlive`):
  - `server/internal/db/rawrepo/stage_run_bulk_repo.go:11,53` (drops the `pipeline` import entirely)
  - `server/internal/api/tasks/enrich.go`
  - `server/internal/api/tasks/analyze_routes.go`
  - `server/internal/pipeline/completion_detector.go`
  - `server/internal/pipeline/orchestrator.go`
  - `server/internal/pipeline/spawner.go`
  - `server/internal/pipeline/progress_guards.go`
  - `server/internal/pipeline/sweeps.go`
  - `server/internal/pipeline/session_manager_test.go`
  - `server/internal/pipeline/completion_detector_test.go`
- `.golangci.yml` — add `depguard` rule.
- `docs/architecture/adr/0009-proc-leaf.md` (new — see ADR section).
- `.agent-context/architecture.md` + `.agent-context/task-pipeline.md` — add `proc` as a leaf
  node in the Go import graph.

### Steps
1. **Failing test.** Create `server/internal/proc/proc_test.go` referencing `proc.IsPidAlive`;
   `cd server && go build ./...` fails (package does not exist yet).
2. **Move.** Create `proc/proc.go`, move `IsPidAlive` + `isPidZombie` verbatim; keep the same
   stdlib imports (`os`, `os/exec`, `syscall`, `strings`). Delete both from
   `session_manager.go`.
3. **Migrate importers.** Replace every `pipeline.IsPidAlive` call site with `proc.IsPidAlive`
   and fix imports. In `stage_run_bulk_repo.go` the `pipeline` import is removed outright —
   confirm no other `pipeline.` symbol remains in that file.
4. **Run.** `cd server && go build ./... && go test ./internal/proc/... ./internal/pipeline/...
   ./internal/db/rawrepo/... ./internal/api/tasks/...`. Note: `go test ./...` regenerates the
   ent tree — restore with `git checkout -- server/internal/db/ent/` afterward.
5. **Lint boundary.** Add to `.golangci.yml`:
   ```yaml
   linters-settings:
     depguard:
       rules:
         no-pipeline-from-infra-or-edge:
           list-mode: lax
           files:
             - "**/internal/db/rawrepo/**"
             - "**/internal/api/**"
           deny:
             - pkg: "github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
               desc: "infra/edge must not import the pipeline orchestration core; use the internal/proc leaf for liveness"
   ```
   Confirm `golangci-lint run` is green after migration and would flag a reintroduced import
   (spot-check by temporarily re-adding the old import in a scratch edit, then revert).
6. **Docs.** Add the `proc` leaf to the two import-graph docs.

**Risk:** wide (11 files incl. 2 test files) but purely mechanical — no signature or behaviour
change. `isPidZombie` shells out (`ps` on macOS); keep its body byte-identical to avoid changing
platform behaviour.

---

## Task 2 — Extract transport-only `ollamaclient` leaf

**Type:** design decision (the shared seam) + move.
**Why:** the Ollama HTTP wire code is duplicated. `llmadapter/llm_ollama.go` (`OllamaSpawner`)
builds a chat request and posts to the API, then writes a synthetic JSONL session file.
`provider/ollama.go` (`OllamaClassifier`) hits `GET /api/tags` with a TTL cache to decide
zero-cost locality. Host normalization, the `http.Client`, and the endpoint contracts are
copied in both. SSOT violation (ARCH-P2-3).

### Files
- `server/internal/ollamaclient/client.go` (new) — `package ollamaclient`:
  - `New(host string) *Client` — trims trailing slash, defaults `http://localhost:11434`,
    owns the `*http.Client`.
  - `Chat(ctx, ChatRequest) (ChatResponse, error)` — `POST /api/chat`.
  - `Tags(ctx) ([]string, error)` — `GET /api/tags`.
  - Types `ChatRequest`/`ChatMessage`/`ChatResponse` (wire shapes only — no JSONL, no cost).
- `server/internal/ollamaclient/client_test.go` (new) — `httptest.Server` covering host
  normalization, chat round-trip, tags parse, unreachable-host → empty/err.
- `server/internal/llmadapter/llm_ollama.go` — `OllamaSpawner` delegates the HTTP call to
  `ollamaclient.Client`; retains JSONL-writing + spawn-result mapping.
- `server/internal/provider/ollama.go` — `OllamaClassifier` delegates `GET /api/tags` to
  `ollamaclient.Client`; retains the TTL cache + `IsLocal` policy.

### Steps
1. **Design gate.** The leaf is **transport only** — no cost logic, no JSONL, no locality
   policy. Callers keep their distinct concerns. This is the seam decision; do not pull the TTL
   cache or JSONL writer into the leaf (they are caller-specific).
2. **Failing test** referencing `ollamaclient.New`; `go build ./...` fails.
3. **Implement** the leaf against `httptest`; both endpoints, host normalization, short client
   timeout parity with the classifier's `800ms` (make timeout a `New` option or field so the
   spawner keeps its longer budget).
4. **Rewire** both callers to the leaf; delete the duplicated request/host code.
5. **Run** `cd server && go build ./... && go test ./internal/ollamaclient/...
   ./internal/llmadapter/... ./internal/provider/...`.

**Risk:** over-abstraction — the two callers legitimately differ (timeout, caching, output).
Keep the leaf minimal; if the timeout/caching split fights the shared client, prefer a thin
`Client` with per-call options over forcing one config.

---

## Task 3 — DI `*ServerComponents` struct (replace 12-value return)

**Type:** mechanical, but touches the fragile bootstrap. **Option A** (recommended in the
brainstorm).
**Why:** `serverapp/di.go:111` — `initializeServer` returns 12 positional values (11 typed
pointers + `func()` + `error`), forcing every early-return site to write
`return nil, nil, nil, ... , err` (15 such sites). Positional nils are order-sensitive and a
misplaced nil compiles silently — a latent-bug generator.

### Files
- `server/serverapp/di.go` — introduce `ServerComponents` struct; change
  `initializeServer` signature to `(*ServerComponents, error)`; update all early returns to
  `return nil, err`; update the single call site to read fields off the struct.

### Steps
1. Define:
   ```go
   type ServerComponents struct {
       API          *api.Server
       Broadcaster  *sse.Broadcaster
       Merger       *merger.Merger
       Orchestrator *pipeline.PipelineOrchestrator
       Scheduler    *scheduler.Scheduler
       HistImporter *histsvc.Importer
       Baseline     agentbroadcast.BaselineProvider
       Enricher     merger.Enricher
       Eval         *eval.Service
       Settings     *settings.Service
       Cleanup      func()
   }
   ```
   (Field names mirror the current positional order — verify each type against the existing
   signature at `di.go:111` before finalizing.)
2. Change the signature to `func initializeServer(...) (*ServerComponents, error)`; convert the
   15 `return nil, nil, ..., err` sites to `return nil, err`; build the struct at the single
   success return.
3. Update the caller to consume `comps.API`, `comps.Cleanup`, etc.
4. **Run** `cd server && go build ./... && go test ./serverapp/...`.
5. **Boot smoke (mandatory).** `di.go` is the in-process serverapp bootstrap on the desktop GUI
   path — a compile pass is not sufficient. Run a real boot (`task dev` or the serverapp
   entrypoint) and confirm the server binds `127.0.0.1:13120`, SSE streams open, and cleanup
   runs on shutdown. The construction order and the outward callback wiring
   (`OrchestratorOptions.OnTaskChanged`, broadcaster injection) must be preserved exactly.

**Risk:** the refactor itself is safe, but a dropped/misordered field assignment would surface
only at runtime — the boot smoke is the guard.

---

## Task 4 — Cross-language slug-parity CI test (SSOT drift guard)

**Type:** mechanical. Template for future cross-language SSOT enforcement.
**Why:** `server/internal/validation/slug.go` (`SlugRE = ^[a-z0-9][a-z0-9-]{0,63}$`) and
`src/utils/validation.ts` (`SLUG_RE`) are hand-synced with only a comment linking them
(ARCH-P2-4). Nothing fails when they drift. Client and server are different languages — no
shared module — so a test is the only enforcement.

### Files
- `src/utils/__tests__/slug-parity.test.ts` (new) — Vitest reads the Go source literal and
  asserts equality with the TS `SLUG_RE.source`.

### Steps
1. In the test, read `server/internal/validation/slug.go` from disk, extract the
   `regexp.MustCompile(\`...\`)` literal via a narrow regex, and assert it equals
   `SLUG_RE.source`. Also assert `SlugPatternMessage` ↔ `SLUG_PATTERN_MESSAGE` parity if both
   exist.
2. Add representative accept/reject cases run against **both** patterns (same inputs, same
   verdicts) so a semantic divergence — not just a textual one — also fails.
3. **Run** `pnpm test`. Confirm the test fails if the Go literal is edited (spot-check, revert).
4. This test is the reusable template: one parity test per hand-synced cross-language constant.
   Document the policy in ADR-0011.

**Risk:** the test reads a sibling-language source file by path — pin the path relative to the
repo root and fail loudly (not skip) if the file is missing, so a moved file surfaces as a red
test rather than a silent pass.

---

## Task 5 — Scope-down amendment to ADR-0004 (`domainerr` adoption)

**Type:** docs-only (XS). **Option B** from the brainstorm.
**Why:** `domainerr` is adopted by ~5 importers, not the ~40 originally implied (ARCH-P3-3).
Driving repo-wide adoption is high-churn, low near-term payoff. Instead, scope the decision to
match reality: `domainerr` is for **cross-boundary sentinels** (db/service → api mapping via
`errors.Is`), not intra-package error vocabulary.

### Files
- `docs/architecture/adr/0004-domain-error-sentinels.md` — append an `## Amendment
  (2026-07-12): Scope` section.

### Steps
1. Append to ADR-0004:
   > **Amendment (2026-07-12) — Scope.** `domainerr` is intentionally scoped to
   > *cross-boundary* error sentinels: values that must satisfy `errors.Is` across a layer
   > edge (typically db/repo or a service returning up to `apierr.ErrorMiddleware`).
   > Intra-package errors should stay local (`fmt.Errorf`/local sentinels); they need not adopt
   > `domainerr`. The low importer count is therefore expected, not drift. Revisit only if a
   > concrete error-mapping pain point surfaces at a boundary that lacks a sentinel.
2. No code change. No new ADR number.

---

## Final Verify
1. `cd server && go build ./... && go vet ./...` — clean.
2. `cd server && go test ./...` then `git checkout -- server/internal/db/ent/` (test run
   regenerates the ent tree).
3. `golangci-lint run` — clean, and the new `depguard` rule is active (verify by temporary
   reintroduction of `internal/pipeline` in `rawrepo`, confirm red, revert).
4. `pnpm lint && pnpm typecheck && pnpm test` — clean, slug-parity test present and green.
5. **Boot smoke** (Task 3): real serverapp start binds `127.0.0.1:13120`, SSE up, clean
   shutdown.
6. `grep -rl 'internal/pipeline' server/internal/db/ server/internal/api/tasks/enrich.go
   server/internal/api/tasks/analyze_routes.go` returns empty for the liveness import.
7. Docs current: `architecture.md` + `task-pipeline.md` show the `proc` and `ollamaclient`
   leaves; ADRs 0009–0012 present; ADR-0004 amended.

**PRs (recommended cut):** Task 1 (+lint+ADR-0009) · Task 2 (+ADR nothing / import-graph) ·
Task 3 · Task 4 (+ADR-0011) · Task 5. ADRs 0010 and 0012 are pure backfill and can ride in
Task 1's PR or a standalone docs PR.
