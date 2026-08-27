# AgenticOS K3 — Memory Store Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the system continuity — knowledge that outlives a session, is scoped to where it applies, records where it came from and when it stops being true, and reaches agents without depending on them asking.

**Architecture:** A structured store in the existing SQLite database, indexed by FTS5 following the pattern `task_fts` already established. Delivery is two-sided: a budgeted extract injected at spawn guarantees a floor, and an MCP search tool serves depth. Writing memory is itself a capability, because it is how one agent changes what every future agent believes.

**Tech Stack:** Go 1.26, ent v0.14.6, modernc SQLite with FTS5, chi. Frontend untouched by this plan.

**Spec:** `docs/superpowers/specs/2026-08-27-agenticos-memory-design.md`
Umbrella: `docs/superpowers/specs/2026-08-27-agenticos-overview-design.md`
Conventions: `docs/superpowers/specs/2026-08-27-agenticos-conventions-design.md`

**Depends on:** K1 (registry — `memory_space` is a registry resource) and K2 (`memory.write` is a capability with grants). Do not start until both are merged.

> **Verify before dispatching any task that consumes K2.** This plan was written before K2 existed, and every K2 symbol it names is an assumption. K1's execution produced five plan defects, every one of them an unverified claim about existing code. Tasks 3, 9 and 10 touch K2; re-read the merged signatures and correct the brief before dispatching them.

## Global Constraints

- **Go workspace** with `./sdk` and `./server`. `go build ./...` from the root FAILS — use `go build ./server/...`.
- **`go test` regenerates the ent tree.** `git status --short server/internal/db/ent/` must be empty before committing unless the regeneration is the change.
- **Never amend a commit.** Add a new one.
- **New unique indexes are pre-created under ent's exact generated name** — read from `server/internal/db/ent/migrate/schema.go`, never guessed — before `Schema.Create`.
- **JSON columns on populated tables need a raw `entsql.Default`.**
- **Empty string, not NULL, is the global-scope sentinel.**
- **Upsert never uses blanket `UpdateNewValues()`** on a table with lifecycle columns. K1 shipped that bug and fixed it in final review.
- **Everything that ships is English.** Conventional Commits, no task or phase references.
- **Gates:** `go build ./server/...`; `go vet ./...` in both modules; `go test -race` over the touched packages; `task test` before the final commit; `gofmt -l` on changed files. `task lint` is broken locally for toolchain reasons; CI is the authority.

## What exists today, verified at plan time

| Fact | Location |
|---|---|
| FTS5 lives in Go, not `.sql` files; `runRawMigrations` runs **after** `Schema.Create` | `server/internal/db/client.go:119` |
| Sync triggers are dropped and recreated on **every boot**, deliberately | `client.go:130-141` |
| `task_fts` is **content-owning**, so plain `DELETE` by rowid is required; the contentless form fails at runtime | `client.go:163-188`, comment at `:176` |
| The FTS query sanitizer, currently single-caller | `server/internal/api/search/handler.go:160` |
| The single point where the final user prompt is assembled | `server/internal/pipeline/stage_handlers.go:213-219` |
| **Silent** system-prompt truncation at 10 000 chars | `server/internal/pipeline/spawner.go:41,173-174` |
| MCP `Register` panics on a missing `ToolScopeMap` entry | `server/internal/mcp/registry.go:55,58` |
| `validKeyScopes`, which omits `agent:coord` | `server/internal/mcp/tools/keys.go:17` |
| Secret scrubbing, applied on read-out only | `server/internal/parser/scrub.go` |
| Trojan-source sanitization; truncation counted, never marked | `server/internal/sanitize/sanitize.go:31-35` |

---

### Task 1: Free the name — rename the config explorer's "memory"

`/api/config/memory` already exists and enumerates `CLAUDE.md` and `AGENTS.md` files. It is a file browser, not a store, and the Memory resource is something else entirely. Renaming first means no task after this one has to disambiguate.

**Files:**
- Modify: `server/internal/api/config/handler.go` — `MemoryEntry` → `ContextFileEntry`, `Memory` → `ContextFiles`, `memoryResponse` → `contextFilesResponse`
- Modify: `server/internal/api/router.go` — add `/api/config/context-files`, keep `/api/config/memory` answering with a deprecation log
- Modify: `src/composables/useConfigExplorer.ts` and any caller — point the client at the new path
- Test: `server/internal/api/config/handler_test.go` (extend)

**Interfaces:**
- Produces: `GET /api/config/context-files`; the old path answering identically for one minor version.

- [ ] **Step 1: Write the failing test**

```go
func TestContextFilesEndpointAnswers(t *testing.T) {
	// GET /api/config/context-files must return the same shape the old
	// /api/config/memory returned. Assert on the payload, not the status code
	// alone — a 200 with an empty body would pass a status-only check.
}

func TestLegacyMemoryPathStillAnswers(t *testing.T) {
	// GET /api/config/memory must still answer during the deprecation window.
	// Removing it in the same release that adds the new path would break any
	// client that has not been rebuilt.
}
```

Write both against the existing handler test's harness.

- [ ] **Step 2: Run red**

Run: `cd server && go test ./internal/api/config/ -run 'ContextFiles|LegacyMemory' -count=1`
Expected: FAIL — the route does not exist.

- [ ] **Step 3: Rename and add the route**

Keep the handler body identical; this is a rename plus an alias, not a rewrite. Log once per process when the legacy path is hit, at info level, naming the replacement.

- [ ] **Step 4: Update the client**

`grep -rn "config/memory" src/` and repoint. Run `pnpm lint && pnpm typecheck && pnpm test`.

- [ ] **Step 5: Verify and commit**

```bash
cd server && go test -race -count=1 ./internal/api/config/
git add server/internal/api/ src/
git commit -m "refactor(api): rename the config explorer's memory endpoint to context files

That endpoint enumerates CLAUDE.md and AGENTS.md files on disk. It was never a
memory store, and the name is about to belong to one.

The old path still answers for a deprecation window and logs once, so a client
built against it keeps working."
```

---

### Task 2: Memory schemas

**Files:**
- Create: `server/internal/db/ent/schema/memory_space.go`, `memory_entry.go`, `memory_injection.go`
- Modify: `server/internal/db/client.go` — index pre-migrations plus call sites
- Test: `server/internal/db/client_test.go` (append)
- Regenerated and committed: `server/internal/db/ent/**`

**Interfaces:**
- Consumes: K1's `IDTimestampsMixin`.
- Produces: ent entities `MemorySpace`, `MemoryEntry`, `MemoryInjection`.

Fields, per spec §3:

```
memory_space:   slug, name, scope_kind, scope_ref (""=global), node_id
                unique (scope_kind, scope_ref, slug)

memory_entry:   space_id, summary, content (Text), kind, source_kind,
                source_ref (nillable), confidence float, valid_from,
                valid_until (nillable), superseded_by (nillable),
                user_id (nillable)
                indexes: (space_id, valid_until), (space_id, kind), (superseded_by)

memory_injection: stage_run_id, entry_ids JSON, char_budget int,
                chars_used int, candidate_count int
```

`summary` is a separate column, not derived from `content`. The push budget is spent on summaries, and a derived first-N-characters would make budget cost depend on how verbosely an agent happened to write.

- [ ] **Step 1: Write the schemas**, mirroring K1's `resource.go` in style.

- [ ] **Step 2: Regenerate and read the generated index names**

Run: `cd server && go generate ./internal/db/ent/`
Run: `grep -n 'memoryspace_\|memoryentry_' server/internal/db/ent/migrate/schema.go`

Use the exact strings you read in Step 4.

- [ ] **Step 3: Write the failing reopen test**

Append to `client_test.go` a test that opens a database, inserts one space and one entry, closes, reopens, and asserts both survive — the same shape as K1's `TestOpenTwiceWithResourceTable`.

- [ ] **Step 4: Add the pre-migrations**, one per unique index, following `migrateEnsureResourceUniqueIndex` in the same file.

- [ ] **Step 5: Verify and commit** (the regenerated tree is the change and gets committed)

```bash
cd server && go build ./... && go vet ./... && go test -race -count=1 ./internal/db/...
git add server/internal/db/
git commit -m "feat(db): add the memory store schema

Entries carry scope, source, validity and confidence, so the store can answer
what it knows, since when, from where, and whether it still holds — none of
which a markdown file can express.

summary is its own column rather than a prefix of content: the injection
budget is spent on summaries, and deriving them would make an agent's
verbosity determine what fits."
```

---

### Task 3: MemoryRepo

**Files:**
- Create: `server/internal/db/repo/memory_repo.go`
- Test: `server/internal/db/repo/memory_repo_test.go`

**Interfaces:**
- Consumes: K1's `repo.Scope`.
- Produces: `repo.MemoryRepo` with `CreateSpace`, `GetSpace`, `ListSpaces`, `CreateEntry`, `GetEntry`, `SupersedeEntry(ctx, oldID, newID string) error`, `ExpireEntry(ctx, id string, at time.Time) error`, `ListValid(ctx, spaceID string, now time.Time) ([]*ent.MemoryEntry, error)`, `RecordInjection`; input structs `CreateSpaceInput`, `CreateEntryInput`, `RecordInjectionInput`.

- [ ] **Step 1: Write the failing test**

```go
func TestListValidExcludesExpiredAndSuperseded(t *testing.T) {
	r, ctx := newMemoryRepo(t)
	space := mustSpace(t, r, ctx, "project-a")
	now := time.Now()
	past := now.Add(-time.Hour)

	live := mustEntry(t, r, ctx, space.ID, "still true")
	expired := mustEntry(t, r, ctx, space.ID, "no longer true")
	if err := r.ExpireEntry(ctx, expired.ID, past); err != nil {
		t.Fatalf("expire: %v", err)
	}
	old := mustEntry(t, r, ctx, space.ID, "replaced")
	replacement := mustEntry(t, r, ctx, space.ID, "replacement")
	if err := r.SupersedeEntry(ctx, old.ID, replacement.ID); err != nil {
		t.Fatalf("supersede: %v", err)
	}

	got, err := r.ListValid(ctx, space.ID, now)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	ids := map[string]bool{}
	for _, e := range got {
		ids[e.ID] = true
	}
	if !ids[live.ID] || !ids[replacement.ID] {
		t.Error("a live entry and a replacement must both be returned")
	}
	if ids[expired.ID] {
		t.Error("an expired entry must not be returned")
	}
	if ids[old.ID] {
		t.Error("a superseded entry must not be returned")
	}
}

func TestSupersedeDoesNotDelete(t *testing.T) {
	r, ctx := newMemoryRepo(t)
	space := mustSpace(t, r, ctx, "project-a")
	old := mustEntry(t, r, ctx, space.ID, "replaced")
	replacement := mustEntry(t, r, ctx, space.ID, "replacement")
	if err := r.SupersedeEntry(ctx, old.ID, replacement.ID); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	got, err := r.GetEntry(ctx, old.ID)
	if err != nil {
		t.Fatalf("the superseded row must still exist: %v", err)
	}
	if got.SupersededBy == nil || *got.SupersededBy != replacement.ID {
		t.Error("superseded_by must point at the replacement — the chain is the audit trail")
	}
}
```

Write the `mustSpace` / `mustEntry` helpers in the test file.

- [ ] **Step 2: Run red, then implement**

`ListValid` filters `valid_until IS NULL OR valid_until > now` **and** `superseded_by IS NULL`. Nothing deletes: contradiction history is what makes the store auditable.

- [ ] **Step 3: Verify and commit**

```bash
cd server && go test -race -count=1 ./internal/db/repo/
git checkout -- server/internal/db/ent/
git add server/internal/db/repo/
git commit -m "feat(db): add the memory repository with supersession and expiry

Superseding writes a pointer on the old row instead of mutating or deleting
it, so the chain of what replaced what survives. Expiry excludes an entry from
retrieval without removing it, for the same reason."
```

---

### Task 4: FTS5 index for memory

**Files:**
- Modify: `server/internal/db/client.go` — extend `runRawMigrations`
- Test: `server/internal/db/client_test.go` (append)

**Interfaces:**
- Produces: the `memory_fts` virtual table and its three sync triggers.

Mirror `task_fts` exactly. Three facts from that implementation govern this one:

- All FTS DDL lives in Go; statements execute one at a time because SQLite rejects multi-statement `Exec`.
- Triggers are dropped and recreated on every boot so a change to a trigger body takes effect on existing databases.
- `task_fts` is **content-owning**, which is why its delete is a plain `DELETE ... WHERE rowid = old.rowid`. The contentless form (`INSERT INTO ft(ft, rowid) VALUES('delete', ...)`) is a runtime error against a content-owning table. `memory_fts` is content-owning too.

- [ ] **Step 1: Write the failing round-trip test**

```go
func TestMemoryFTSRoundTrip(t *testing.T) {
	// Insert an entry, assert a MATCH finds it.
	// Update its summary, assert the old term no longer matches and the new one does.
	// Delete it, assert the MATCH returns nothing.
	//
	// The delete leg is the one that matters: a contentless-form trigger against
	// a content-owning table fails at runtime, not at boot, so only an actual
	// delete exercises it.
}
```

Write it out against `db.Open(":memory:")` and the raw `*sql.DB` from the bundle, following `client_test.go`'s existing FTS round-trip test.

- [ ] **Step 2: Run red, then add the DDL**

```sql
CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts USING fts5(
    entry_id UNINDEXED,
    summary,
    content
)
```

Plus `memory_entries_ai`, `_au`, `_ad` triggers in the same shape as `tasks_ai/au/ad`, with the drops preceding them.

- [ ] **Step 3: Verify and commit**

```bash
cd server && go test -race -count=1 ./internal/db/
git add server/internal/db/client.go server/internal/db/client_test.go
git commit -m "feat(db): index memory entries with FTS5

Follows the task_fts pattern exactly: content-owning, triggers dropped and
recreated each boot so trigger changes reach existing databases, and a plain
rowid DELETE because the contentless delete form is a runtime error against a
content-owning table."
```

---

### Task 5: One FTS query sanitizer

The sanitizer has one caller today. Memory retrieval will be the second. Moving it now, while it is still free, is the whole point of the project's single-source-of-truth rule.

**Files:**
- Create: `server/internal/db/rawrepo/ftsquery.go` (or `server/internal/search/` — choose and say why in the commit)
- Modify: `server/internal/api/search/handler.go` — delegate
- Test: `server/internal/db/rawrepo/ftsquery_test.go`

**Interfaces:**
- Produces: `SanitizeFTSQuery(raw string) string`, exported.

- [ ] **Step 1: Write the failing test**, pinning the existing behaviour: each whitespace token becomes a quoted prefix match, and characters that would break FTS5 syntax are neutralised. Copy the cases from the current implementation's behaviour, then add one for an input that is only punctuation.

- [ ] **Step 2: Run red, move the function, delegate from the old site, verify both callers**

- [ ] **Step 3: Commit**

```bash
git commit -m "refactor(search): move the FTS query sanitizer to a shared home

It had one caller and is about to have two. Moving it while it is still
single-caller is free; copying it later would be a second implementation of a
rule that must not diverge."
```

---

### Task 6: Ingest sanitization

**Files:**
- Create: `server/internal/memory/ingest.go`
- Test: `server/internal/memory/ingest_test.go`

**Interfaces:**
- Consumes: `parser` secret scrubbing, `sanitize.ForDisplayCapped`.
- Produces: `memory.SanitizeForStore(summary, content string) (string, string, error)`; sentinel `memory.ErrEmptyAfterSanitize`.

Three rules, each from the spec and each with a reason the codebase already learned:

- **Secrets are scrubbed at write, not at read.** The existing scrubber runs on read-out before API exposure. A store is persistent: a secret written into it survives every later scrub.
- **Trojan-source sanitization applies here.** `sanitize`'s own package doc records that this rule existed at one boundary, a second was added later without inheriting it, and the protection sat on the passive trail while the text next to the approve button stayed raw. Memory is a third boundary — rendered in the UI *and* concatenated into prompts.
- **Truncation is counted, never marked.** A marker inside the text is one the text can forge.

- [ ] **Step 1: Write the failing test**

```go
func TestSanitizeForStoreRedactsSecrets(t *testing.T) {
	_, content, err := memory.SanitizeForStore("summary", "the token is sk-ant-api03-REDACTMEREDACTMEREDACTMEREDACTME")
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}
	if strings.Contains(content, "sk-ant-api03-REDACTMEREDACTMEREDACTMEREDACTME") {
		t.Error("a secret must not reach the store — it is persistent, and later scrubbing cannot help")
	}
}

func TestSanitizeForStoreStripsBidiControls(t *testing.T) {
	_, content, err := memory.SanitizeForStore("summary", "safe‮txet desrever‬")
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}
	if strings.ContainsRune(content, '‮') {
		t.Error("bidi override must be stripped — this text is rendered and concatenated into prompts")
	}
}

func TestSanitizeForStoreRefusesWhenEmptied(t *testing.T) {
	_, _, err := memory.SanitizeForStore("", "‮‬")
	if !errors.Is(err, memory.ErrEmptyAfterSanitize) {
		t.Fatalf("err = %v, want ErrEmptyAfterSanitize — a silently emptied entry is worse than a rejected one", err)
	}
}
```

- [ ] **Step 2: Run red, then implement** by composing the existing scrubber and sanitizer rather than writing new filters.

- [ ] **Step 3: Verify and commit**

```bash
git commit -m "feat(memory): sanitize entries at write rather than at read

The secret scrubber runs on read-out before API exposure. A store is
persistent, so a secret written into it outlives every later scrub — this
boundary has to be the write.

An entry emptied by sanitization is refused rather than stored blank: a
silently empty memory is worse than a rejected write, because nothing surfaces
that anything was lost."
```

---

### Task 7: Retrieval scoring

**Files:**
- Create: `server/internal/memory/score.go`
- Test: `server/internal/memory/score_test.go`

**Interfaces:**
- Produces: `memory.Candidate{EntryID string; Lexical float64; ScopeKind string; CreatedAt time.Time; Confidence float64; Kind string}`; `memory.Score(c Candidate, now time.Time) float64`; `memory.Rank(cs []Candidate, now time.Time) []Candidate`.

A weighted composite over a bounded candidate set. The pattern already exists in this codebase — `server/internal/merger/health.go` is the only weighted-sum scorer, with normalised components, fixed weights summing to 1.0, a neutral value for missing inputs and a hard cap. Follow that shape so there is one scoring idiom, not two.

Components, per spec §6: lexical relevance (FTS5 `bm25`), scope specificity (application > project > global), recency (decayed), stored confidence, and a kind weight placing `preference` and `lesson` above `fact` for the push.

- [ ] **Step 1: Write the failing test**

```go
func TestScopeSpecificityOutranksEqualRelevance(t *testing.T) {
	now := time.Now()
	global := memory.Candidate{EntryID: "g", Lexical: 0.5, ScopeKind: "global", CreatedAt: now, Confidence: 0.8, Kind: "fact"}
	project := memory.Candidate{EntryID: "p", Lexical: 0.5, ScopeKind: "project", CreatedAt: now, Confidence: 0.8, Kind: "fact"}
	if memory.Score(project, now) <= memory.Score(global, now) {
		t.Error("with equal lexical relevance the project-scoped entry must win")
	}
}

func TestRankIsDeterministic(t *testing.T) {
	// Rank the same slice twice, in two different input orders, and assert the
	// output order is identical. A scorer with ties resolved by map iteration
	// would pass a single-run test and fail this one.
}

func TestPreferenceOutranksFactAtEqualScoreElsewhere(t *testing.T) {
	// A preference changes behaviour; a fact is usually looked up on demand.
	// The push budget should prefer the one that changes what the agent does.
}
```

Write the second and third out fully.

- [ ] **Step 2: Run red, implement, verify**

- [ ] **Step 3: Commit**

```bash
git commit -m "feat(memory): rank retrieval candidates by a weighted composite

Follows the one weighted-sum idiom this codebase already has — normalised
components, fixed weights, a neutral value for missing inputs — rather than
introducing a second shape.

Ranking is deterministic under reordering: ties broken by map iteration would
make the injected set differ between runs on identical data."
```

---

### Task 8: Push at spawn

This is the task with a trap in it. Read the whole thing before starting.

**Files:**
- Create: `server/internal/memory/inject.go`
- Modify: `server/internal/pipeline/stage_handlers.go` — the user-prompt assembly at `buildStageUserPrompt`
- Test: `server/internal/memory/inject_test.go`, `server/internal/pipeline/stage_handlers_test.go` (extend)

**Interfaces:**
- Produces: `memory.BuildBlock(entries []Entry, budget int) (block string, used int, dropped int)`; `memory.InjectorFunc` wired into `StageContext`.

**The seam is the user prompt, not the system prompt.** `server/internal/pipeline/spawner.go:41,173-174` truncates the system prompt at 10 000 characters **silently**, head-first. Custom system-prompt content is *prepended*, so a memory block injected there would push the actual stage instructions past the cut and delete them. The failure would be invisible: a well-formed prompt that no longer says what to do.

`buildStageUserPrompt` at `stage_handlers.go:213-219` is the single point where the final user prompt is assembled, and both the native path and the LLM-adapter path consume its result. That is where the block goes — appended after the stage instructions and before the user's additional prompt, so instructions keep primacy and human input keeps the last word.

**The budget is in characters, deliberately.** There is no pre-flight token counting anywhere in this codebase; token accounting is entirely after the fact. Introducing a tokenizer for one feature is disproportionate. The budget bounds cost; it does not compute it, and the plan says so rather than implying precision.

- [ ] **Step 1: Write the failing test**

```go
func TestBuildBlockNeverExceedsBudget(t *testing.T) {
	entries := make([]memory.Entry, 50)
	for i := range entries {
		entries[i] = memory.Entry{Summary: strings.Repeat("x", 100)}
	}
	block, used, dropped := memory.BuildBlock(entries, 500)
	if len(block) > 500 {
		t.Errorf("block is %d chars, budget was 500", len(block))
	}
	if used > 500 {
		t.Errorf("used = %d, budget was 500", used)
	}
	if dropped == 0 {
		t.Error("50 entries of 100 chars cannot fit in 500 — dropped must be non-zero")
	}
}

func TestBuildBlockEmitsNothingForNoEntries(t *testing.T) {
	block, used, dropped := memory.BuildBlock(nil, 500)
	if block != "" || used != 0 || dropped != 0 {
		t.Errorf("empty input must produce no block at all, got %q", block)
	}
}

func TestBuildBlockDoesNotMarkTruncation(t *testing.T) {
	entries := []memory.Entry{{Summary: strings.Repeat("x", 1000)}}
	block, _, dropped := memory.BuildBlock(entries, 100)
	if strings.Contains(block, "truncated") || strings.Contains(block, "…") {
		t.Error("truncation is counted, not marked — a marker inside the text is one the text can forge")
	}
	if dropped != 1 {
		t.Errorf("dropped = %d, want 1", dropped)
	}
}
```

The second test carries a real decision: an empty section invites the model to fill it. No entries means no block, not an empty heading.

- [ ] **Step 2: Run red, then implement `BuildBlock`**

- [ ] **Step 3: Write the seam test**

In `stage_handlers_test.go`, assert the block appears in the final **user** prompt for both the native and the adapter path, and add a regression test asserting nothing was written into the system prompt. That second assertion is what stops a future refactor from moving the injection into the truncation trap.

- [ ] **Step 4: Wire it and record the injection**

Every injection writes a `memory_injection` row: which entries, the budget, what was used, how many candidates were considered. Without it the scoring heuristic can never be improved, only argued about.

- [ ] **Step 5: Verify and commit**

```bash
cd server && go test -race -count=1 ./internal/memory/ ./internal/pipeline/
git commit -m "feat(memory): inject a budgeted extract into the spawn user prompt

The user prompt is the seam, not the system prompt: system-prompt content is
truncated silently at 10000 characters head-first, and since custom content is
prepended, a block there would delete the stage instructions and leave a
well-formed prompt that no longer says what to do.

The budget is in characters. There is no pre-flight token counting in this
codebase and adding a tokenizer for one feature is disproportionate — the
budget bounds cost rather than computing it, and this says so instead of
implying precision.

Every injection is recorded, because a retrieval heuristic nobody can measure
can only be argued about."
```

---

### Task 9: MCP tools

**Files:**
- Create: `server/internal/mcp/tools/memory.go`
- Modify: `server/internal/mcp/auth.go` — `ToolScopeMap` and `scopeImplies`
- Modify: `server/internal/mcp/tools/keys.go` — `validKeyScopes`
- Modify: `server/serverapp/di_mcp.go` — register the group
- Test: `server/internal/mcp/tools/memory_test.go`

**Interfaces:**
- Produces: MCP tools `memory_search` (scope `memory:read`) and `memory_write` (scope `memory:write`).

**A new scope must be added in three places, and missing one fails differently each time:**

1. `ToolScopeMap` (`auth.go:18`) — omitted, `Register` **panics at construction** and the server does not boot.
2. `scopeImplies` (`auth.go:52`) — the expansion is **one level deep, not transitive**. It is complete today only because `keys:manage` enumerates everything explicitly, so a new scope must be added to every implying scope by hand.
3. `validKeyScopes` (`keys.go:17`) — omitted, the scope exists but no API key can be granted it directly. `agent:coord` is in exactly that state today.

**Two authorization layers, deliberately.** The MCP scope authorises the caller's *key* to use the transport. The capability grant authorises the *action against a specific space*. A key holding `memory:write` still cannot write into a space its context has no grant for. *(Verify K2's `Decider` signature before writing this.)*

- [ ] **Step 1: Write the failing test**

```go
func TestMemoryToolsHaveScopeEntries(t *testing.T) {
	for _, name := range []string{"memory_search", "memory_write"} {
		if _, ok := mcp.ToolScopeMap[name]; !ok {
			t.Errorf("%s has no ToolScopeMap entry — Register panics at construction without one", name)
		}
	}
}

func TestMemoryScopesAreGrantableToKeys(t *testing.T) {
	// Assert both new scopes appear in the set an API key may be granted.
	// agent:coord is the cautionary case: it is gated but ungrantable, so it
	// can only be reached through implication.
}

func TestKeysManageImpliesMemoryScopes(t *testing.T) {
	// scopeImplies is one level deep. Assert the expansion of keys:manage
	// contains both memory scopes explicitly.
}
```

Write the second and third out fully.

- [ ] **Step 2: Run red, then implement**

Follow `server/internal/mcp/tools/coord.go` for a tool with real arguments: schema with `required`, argument extraction via the `mcp.StringArg` family, `mcp.OK` / `mcp.Fail`.

- [ ] **Step 3: Verify and commit**

```bash
cd server && go build ./... && go test -race -count=1 ./internal/mcp/...
git commit -m "feat(mcp): add memory search and write tools

A new scope has to be added in three places and each omission fails
differently: no ToolScopeMap entry panics the server at construction, no
scopeImplies entry silently breaks the one-level expansion, and no
validKeyScopes entry leaves the scope gated but ungrantable — which is where
agent:coord sits today.

The MCP scope authorises the key; the capability grant authorises the action
against a specific space. A key with memory:write still cannot write where its
context has no grant."
```

---

### Task 10: HTTP API

**Files:**
- Create: `server/internal/api/memory/handler.go`
- Modify: `server/internal/api/router.go`
- Test: `server/internal/api/memory/handler_test.go`

**Interfaces:**
- Produces: `GET/POST /api/memory/spaces`; `GET/POST /api/memory/entries`; `PATCH/DELETE /api/memory/entries/{id}`; `GET /api/memory/injections?stageRun=`.

**Row scoping is per query, not middleware.** There is no shared row-level-security helper in this codebase; the search repository writes the predicate inline. Memory queries carry the same predicate written the same way, so the pattern stays greppable rather than becoming invisible.

**Security posture is inherited, not invented.** With the default `auth.mode=none` the admin check is a pass-through and the effective actor is any local process reaching the loopback port with a matching Origin. These routes sit on that posture and add no new exposure — and must not become the reason anyone binds the server to a non-loopback address.

- [ ] **Step 1: Write the failing test**, including one asserting a second user's entries never appear.

- [ ] **Step 2: Run red, implement, verify**

- [ ] **Step 3: Commit**

---

### Task 11: Documentation and the full gate

**Files:**
- Modify: `PRIVACY.md`, `CHANGELOG.md`, `README.md`, `docs/README.md`

**`PRIVACY.md` currently states: "There is no automatic expiry for any persisted data — rows live until you delete them."** Memory introduces expiry. That sentence changes, and the per-table disclosure list gains `memory_spaces`, `memory_entries` and `memory_injections`. Stale docs are a defect in this project, not a follow-up.

- [ ] **Step 1: Update the documentation**, including the expiry sentence.
- [ ] **Step 2: Run the full gate**, pasting raw output.
- [ ] **Step 3: Verify `git status --short server/internal/db/ent/` is empty.**
- [ ] **Step 4: Commit.**
- [ ] **Step 5: Stop.** No push, no pull request — both belong to the human.

---

## Out of scope for this plan

| Item | Where it belongs |
|---|---|
| Embedding-based retrieval | Adds a model dependency and an index-refresh problem for a corpus small by construction |
| Automatic contradiction detection | Needs a model call in the write path of every write |
| Obsidian as a memory source | Plan 4 — it arrives with the application |
| Cross-node memory sync | V2, with the node registry |
| Removing the deprecated `/api/config/memory` alias | A later release, after clients have rebuilt |
| A memory UI | Nothing consumes the API yet; the shell is MLP |
