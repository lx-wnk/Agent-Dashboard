# AgenticOS — Memory

**Date:** 2026-08-27
**Status:** Approved design
**Stage:** MVP (unit K3)
**Parent:** `2026-08-27-agenticos-overview-design.md`
**Implements:** decision D4 (the system owns memory) and D7 (budgeted push plus pull)

---

## 1. Purpose

Give the system continuity: knowledge that outlives a session, is scoped to where it applies,
carries where it came from and when it stops being true, and reaches agents without depending on
them asking.

Today there is nothing. What stands in for memory is unstructured markdown that a human curates
by hand — `CLAUDE.md`, `AGENTS.md`, `.agent-context/memory/*.md` — plus per-project auto-memory
files. None of it can express scope, expiry, provenance or contradiction, and all of it is paid
for in full tokens in every session that loads it.

### Non-goals

- **Not a note-taking system.** Obsidian is that, and it is an Application (see the Obsidian
  slice spec). Memory may index notes as a *source*; it does not replace them.
- **Not a vector store.** Retrieval is lexical plus structural scoring on SQLite FTS5. Embeddings
  would add a model dependency and an index-refresh problem for a corpus that is small by
  construction.
- **Not a transcript archive.** Session content already lives in JSONL on disk and is explicitly
  never rewritten by the dashboard (`PRIVACY.md:63`). Memory holds conclusions, not conversations.
- **Not shared across users.** Row scoping matches the existing task pattern; team spaces are
  deferred with everything else team-shaped.

---

## 2. A name collision that must be handled first

`/api/config/memory` **already exists and means something entirely different.** It enumerates
`CLAUDE.md` and `AGENTS.md` files on disk for the config explorer — a file browser, not a store:

- `server/internal/api/config/handler.go:46-53` — `MemoryEntry` carries only path, scope, size,
  mtime and editable
- `server/internal/api/config/handler.go:117-125,132-175` — the handlers
- `server/internal/api/router.go:480` — the route
- `server/internal/cmdscope/enumerate.go:95` — `/memory` is also a built-in slash command name

**Rules for this spec:**

- New endpoints live under `/api/memory/*`. They never extend the config explorer's routes.
- The config-explorer concept is renamed to *context files* — `/api/config/context-files`,
  `ContextFileEntry` — with the old path answering and logging for one minor version. Renaming
  existing constructs is permitted, and "memory" was never an accurate name for a list of markdown
  files on disk. The full vocabulary agreement is in the conventions spec.

This is the same class of mistake as reusing "drift" (see overview §10). Names in this codebase
are already spoken for more often than one expects.

---

## 3. Data model

### 3.1 `memory_space`

A named, scoped namespace. Spaces are registry resources (`kind = memory_space`), which is what
grants attach to: `memory.write` is granted against a space, never against the store as a whole.

| Field | Type | Notes |
|---|---|---|
| `id` | string, immutable | Same id convention as every other entity |
| `slug` | string | Validated by `server/internal/validation/slug.go` — the existing canonical rule |
| `name` | string | Display name |
| `scope_kind` | string | `global` \| `project` \| `application` |
| `scope_ref` | string | Project cwd, or application id. **Empty string for `global`** — the repo's existing sentinel, documented at `server/internal/db/ent/schema/pipeline_config.go:21-23`: "Using a sentinel instead of NULL so the unique index (project_id, key) fires correctly on SQLite." The `*string`-to-sentinel mapping already has one helper, `scopeID` (`repo/pipeline_config_repo.go:55-62`) |
| `node_id` | string | `local` in the MVP (overview D2) |
| `created_at` / `updated_at` | time | Standard |

Unique index on `(scope_kind, scope_ref, slug)`.

> **Migration hazard.** Adding a UNIQUE index through ent triggers SQLite's 12-step table rebuild,
> which has already crashed on existing databases with `NOT NULL constraint failed: ...id`
> (`server/internal/db/client.go:89-96`, PR #207). Five hand-written pre-migrations exist purely
> to dodge this (`client.go:67,75,85,93,109`). New tables with unique indexes follow the same
> pattern: the index is created in the pre-migration path, idempotently, not left to ent alone.

### 3.2 `memory_entry`

| Field | Type | Notes |
|---|---|---|
| `id` | string, immutable | |
| `space_id` | string | FK to `memory_space` |
| `summary` | string | One line. **This is what gets pushed**; `content` is what gets pulled |
| `content` | text | The full entry |
| `kind` | string | `fact` \| `preference` \| `lesson` \| `entity` \| `pointer` |
| `source_kind` | string | `agent` \| `user` \| `application` \| `import` |
| `source_ref` | string, nillable | Stage-run id, application id, file path — whatever identifies the origin |
| `confidence` | float | 0.0–1.0 |
| `valid_from` | time | Defaults to creation |
| `valid_until` | time, nillable | Null means open-ended |
| `superseded_by` | string, nillable | Id of the entry that replaced this one |
| `user_id` | string, nillable | Row scoping, mirroring tasks |
| `created_at` / `updated_at` | time | |

Indexes: `(space_id, valid_until)`, `(space_id, kind)`, `(superseded_by)`.

**Why `summary` is a separate column and not derived.** The push budget is spent on summaries; a
derived first-N-characters would make budget cost depend on how verbosely an agent wrote the
entry. Making it explicit also makes it reviewable in the UI.

**Row scoping is per-query, not middleware.** There is no shared row-level-security helper in this
codebase; the search repository does it inline
(`server/internal/db/rawrepo/search_repo.go:43` — `AND (t.user_id IS NULL OR t.user_id = ? OR ? = 1)`).
Memory queries carry the same predicate, written the same way, so the pattern stays greppable.

### 3.3 `memory_injection`

The record of what was actually pushed into a spawn. Without it the retrieval heuristic can never
be improved, only argued about.

| Field | Type | Notes |
|---|---|---|
| `id` | string, immutable | |
| `stage_run_id` | string | What received the push |
| `entry_ids` | JSON array | Which entries were selected |
| `char_budget` | int | The budget in force |
| `chars_used` | int | What was actually emitted |
| `candidate_count` | int | How many entries were considered before ranking |
| `created_at` | time | |

---

## 4. Full-text search

Follow the existing FTS5 pattern exactly rather than inventing a second one.

The reference implementation is `task_fts` in `server/internal/db/client.go:156-182`. Facts that
matter:

- All FTS DDL lives in Go. There are no `.sql` migration files. `runRawMigrations(sqlDB)` runs
  **after** ent's `Schema.Create` (`client.go:97`, `client.go:119`).
- Statements execute one at a time — SQLite rejects multi-statement `Exec`
  (`client.go:126-128`).
- Sync triggers are **dropped and recreated on every boot**, deliberately, so that changes to a
  trigger body take effect on existing databases (`client.go:130-141`).
- `task_fts` is **content-owning**, not contentless. This determines the delete syntax in the sync
  triggers, and getting it wrong produces `SQLITE_ERROR`. The codebase states it explicitly at
  `client.go:169-172`.

`memory_fts` mirrors this: content-owning, indexing `summary` and `content`, with
`entry_id UNINDEXED`, and three triggers (`ai`, `au`, `ad`) in the same shape.

**One deliberate divergence.** The existing task query orders by nothing at all
(`search_repo.go:36-44`) — it over-fetches `limit*4` and truncates. That is acceptable for a
spotlight search a human reads; it is not acceptable for an automated push where the top N is all
the agent will ever see. Memory retrieval therefore uses FTS5's `bm25()` ranking as one scoring
component (§6).

**Query sanitization is reused, not rewritten.** `sanitizeFtsQuery`
(`server/internal/api/search/handler.go:160-171`) turns each whitespace token into a quoted
prefix match. Per the project's SSOT rule it moves to a shared location rather than being copied.

---

## 5. Ingest

### 5.1 Who may write

Writing memory is how one agent changes what every future agent believes. It is a capability
(`memory.write`), granted against a space, and it is checked by the capability gate before the
store is touched. See the capability-gate spec.

Sources:

| Source | Path | Notes |
|---|---|---|
| Agent | MCP tool `memory_write` | The common case |
| User | `POST /api/memory/entries` | Manual curation and correction |
| Application | Server-internal call | E.g. the Obsidian application indexing a note as a `pointer` |
| Import | One-off migration | Existing `.agent-context/memory/*.md` content, human-reviewed, not automatic |

### 5.2 Sanitization at write, not at read

This is a correctness requirement, not a nicety, and the codebase already learned the lesson the
hard way.

- **Secret scrubbing.** `server/internal/parser/scrub.go` redacts secrets — twelve patterns,
  deliberately conservative about base64 (`scrub.go:32-34`). It runs **on read-out, before API
  exposure**. A memory store fed from session content must apply it **at ingest**, because the
  store is persistent and a secret written into it survives every later scrub.
- **Trojan-source sanitization.** `server/internal/sanitize/sanitize.go:1-13` documents exactly
  this failure: the rule existed at one boundary, a second boundary was added later and did not
  inherit it, "so the protection sat on the passive trail in the modal while the text next to the
  approve button stayed raw." Memory is a third boundary — it is rendered in the UI *and*
  concatenated into prompts. It applies sanitization at ingest.
- **Truncation is counted, not marked.** `sanitize.go:31-35`: a marker inside the text is one the
  text can forge. Memory content truncation follows the same rule.

### 5.3 What must never be stored

`server/internal/hookstore/store.go:1-10` states the agent-side dual-persistence rule: sensitive
tool payloads are never written to disk or a database, which is why hook events live only in a
bounded in-memory ring.

Memory does not weaken this. Entries are conclusions written deliberately, never automatic
captures of tool payloads. There is no "record everything the agent saw" mode, and adding one
later would require revisiting that rule explicitly rather than by accident.

Note also the MCP metadata allow-list (`server/internal/mcp/tools/write.go:628-657`), which
restricts what MCP callers may put into task metadata specifically so security-relevant keys
cannot be escalated. Memory writes go through their own tool with their own capability check;
they do not route through task metadata.

---

## 6. Retrieval and scoring

A weighted composite over a bounded candidate set. The pattern already exists in this codebase —
`server/internal/merger/health.go:9-22,40-68` is the only weighted-sum scorer, with normalised
components, fixed weights summing to 1.0, a neutral value for missing inputs and a hard cap. The
same shape is used here so there is one scoring idiom, not two.

| Component | Source | Rationale |
|---|---|---|
| Lexical relevance | FTS5 `bm25()` against the task title and description | The only signal about *this* piece of work |
| Scope specificity | Application space > project space > global space | A project-specific fact beats a general one |
| Recency | `created_at`, decayed | Recent conclusions usually supersede older ones |
| Confidence | The stored value | Explicitly recorded, so it should count |
| Kind weight | `preference` and `lesson` rank above `fact` for the push | Preferences change behaviour; facts are usually looked up on demand |

Candidate scanning is bounded, following `server/internal/analytics/common.go:19-37`
(`ScanOpts`, `DefaultMaxSessions = 20`) as the precedent for bounding a scan rather than trusting
corpus size.

Entries with `valid_until` in the past, or a non-null `superseded_by`, are excluded from
retrieval. They are not deleted — the history is what makes contradiction visible.

---

## 7. Delivery

### 7.1 Push at spawn — and the trap that decides where

There are two possible injection seams, and the choice is forced by an existing hazard.

**System-prompt seam** — `server/internal/pipeline/stage_handlers.go:31-33`, where DB-backed
custom prompts are already prepended:

```go
if custom := buildCustomSystemPrompt(ctx, h.stage); custom != "" {
    bundle.SystemPrompt = custom + "\n\n---\n\n" + bundle.SystemPrompt
}
```

Semantically this is the right home for "what you know". **It is nevertheless rejected**, because
of `server/internal/pipeline/spawner.go:41,171-177`: `systemPromptMaxChars = 10000`, applied as a
**silent** head-truncation. Since custom content is *prepended*, a large memory block would push
the actual stage instructions past the cut and silently delete them. The failure would be
invisible: the agent receives a well-formed prompt that simply no longer tells it what to do.

**User-prompt seam** — `stage_handlers.go:212-218`, the single point where the final user prompt
is assembled:

```go
return feedback + userPrompt + buildAdditionalPromptSuffix(ctx.UserAdditionalPrompt)
```

Both the native path (`stage_handlers.go:158`) and the LLM-adapter path
(`stage_handlers.go:85-94`) consume the result, so one insertion covers every engine. **This is
the chosen seam.**

The memory block is appended as a clearly delimited section after the stage instructions and
before the user's additional prompt, so that instructions retain primacy and human input retains
the last word.

### 7.2 The budget is in characters, and that is a deliberate compromise

There is **no pre-flight token counting anywhere in this codebase.** Token and cost accounting is
real but entirely after the fact: `pricing.EstimateCost` (`server/internal/pricing/pricing.go:53-60`)
runs against JSONL usage, and budget enforcement is a post-hoc kill —
`orchestrator.go:516-549,570-574` SIGKILLs the process once the accumulated sum exceeds the limit.
The `usage.budget.*` settings (`server/internal/settings/registry.go:127-128`) are display-only
and enforce nothing.

Introducing a tokenizer for one feature is disproportionate. The push budget is therefore a
character budget with a documented approximate ratio, defaulting conservatively, configurable
through the existing settings registry.

Two consequences to record honestly:

- The budget is approximate. It bounds cost; it does not compute it.
- On the native path the whole user prompt travels through `argv`
  (`server/internal/pipeline/spawner.go:158`), so `ARG_MAX` is a real ceiling that the codebase
  acknowledges only for security reasoning (`spawner.go:244`), never for size. The default budget
  sits well below it, and the budget is validated against a maximum rather than accepted freely.

### 7.3 Pull — the `memory_search` MCP tool

Registration mechanics are strict and worth stating, because getting them wrong takes the server
down at boot rather than failing softly:

- `server/internal/mcp/registry.go:51-61` — `Register` **panics** on a duplicate name or on a
  missing `ToolScopeMap` entry. Both are treated as programming errors.
- `server/internal/mcp/jsonrpc.go:76-82` — a tool absent from `ToolScopeMap` resolves to the empty
  scope, which no key ever holds, producing permanent denial. The panic exists to make that state
  unreachable.

New scopes: `memory:read` and `memory:write`. Adding a scope means touching **three** places, and
missing one produces a silent failure in a different direction each time:

1. `ToolScopeMap` (`server/internal/mcp/auth.go:18-50`) — omitted, the tool is ungated and
   `Register` panics at construction.
2. `scopeImplies` (`auth.go:52-58`) — the expansion is **one level deep**, not transitive. It is
   complete today only because `keys:manage` enumerates everything explicitly, so a new scope must
   be added to every implying scope by hand.
3. `validKeyScopes` (`server/internal/mcp/tools/keys.go:17-21`) — omitted, the scope exists but no
   API key can ever be granted it directly. `agent:coord` is in exactly that state today,
   reachable only through implication.

**Two authorization layers, deliberately.** The MCP scope authorises the *caller's key* to use the
transport. The capability grant authorises the *action against a specific space*. A key with
`memory:write` still cannot write into a space its context has no grant for.

### 7.4 Why both

Push alone means every session pays for knowledge it may not need, and the block grows
monotonically — the failure mode of today's `CLAUDE.md` approach.

Pull alone depends on the agent choosing to ask, and it goes silent when the MCP transport is
unavailable — not a hypothetical: a configured MCP server in this very project was refusing
connections while this design was being written. The agent does not notice a missing tool; it
proceeds uninformed.

The push guarantees a floor. The pull provides depth. Neither is load-bearing alone.

---

## 8. Expiry, supersession and forgetting

- `valid_until` in the past excludes an entry from retrieval. Nothing deletes it.
- Writing an entry that contradicts an existing one sets `superseded_by` on the old entry rather
  than mutating it. The chain is the audit trail.
- Contradiction detection in the MVP is **explicit only**: the writer names what it supersedes.
  Automatic contradiction detection is deferred — it needs semantic comparison, which needs a
  model call, which puts a model in the write path of every memory write.
- Hard deletion exists as an explicit user action, because the alternative is a store the user
  cannot correct.

> **Privacy documentation changes.** `PRIVACY.md:54` currently states "There is **no automatic
> expiry** for any persisted data — rows live until you delete them." Memory introduces expiry,
> so that sentence changes. The per-table disclosure list at `PRIVACY.md:34-44` gains three rows.
> Per project rules this happens in the same change, not afterwards
> (`.agent-context/layer2-project-core.md:14`: "Stale docs are a defect, not a follow-up").

---

## 9. API surface

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/memory/spaces` | List spaces visible to the caller |
| `POST` | `/api/memory/spaces` | Create a space |
| `GET` | `/api/memory/entries?space=&q=&kind=` | Browse and search |
| `POST` | `/api/memory/entries` | Manual write |
| `PATCH` | `/api/memory/entries/{id}` | Correct, supersede, or set validity |
| `DELETE` | `/api/memory/entries/{id}` | Hard delete |
| `GET` | `/api/memory/injections?stageRun=` | What was pushed, and what it cost |

MCP tools: `memory_search` (`memory:read`), `memory_write` (`memory:write`).

**Security posture inherited, not invented.** With the default `auth.mode=none`, admin checks are
a pass-through and the effective actor is any local process reaching the loopback port with a
matching Origin — spelled out at `server/internal/pipeline/spawner.go:244`. Memory routes sit on
that same posture. They add no new exposure, and they must not be the reason someone binds the
server to a non-loopback address (`server/internal/config/config.go:141-152` refuses that by
default for exactly this class of data).

---

## 10. Failure modes

| Situation | Behaviour |
|---|---|
| MCP unavailable | The push already happened at spawn. The agent has a floor; only depth is lost |
| FTS query malformed | Sanitizer normalises it. A failing FTS query yields empty results rather than a 500 — matching the existing search handler's stance (`api/search/handler.go:129-132`) |
| Budget exceeded by candidates | Entries are dropped by score, lowest first. The count dropped is recorded in `memory_injection`, not marked inside the text |
| Space has no entries | No block is emitted at all. An empty section is worse than none: it invites the model to fill it |
| Contradictory entries both valid | Both are shown, both scored. Fabricating a winner would hide the problem the store exists to make visible |
| Write denied by capability | The tool returns a denial the agent can act on, not a silent no-op |
| Sanitizer strips the whole entry | The write fails loudly. A silently emptied memory entry is worse than a rejected one |

---

## 11. Testing

- **Store semantics** — table-driven Go tests for validity windows, supersession chains, and
  exclusion rules. A superseded entry must never surface in retrieval.
- **FTS round trip** — insert, update and delete through the triggers, mirroring the existing
  proof at `server/internal/db/client_test.go:136-153`. Explicitly assert the content-owning
  delete form, since the contentless form fails at runtime rather than at compile time.
- **Scoring** — fixture corpus with a known expected ordering; a test that a project-scoped entry
  outranks a global entry with equal lexical relevance.
- **Budget** — property-style test that the emitted block never exceeds the configured budget for
  any corpus, and that dropped entries are counted in `memory_injection`.
- **Seam** — a test that the memory block appears in the final user prompt for both the native and
  the adapter path, and a regression test that nothing is written into the system prompt, because
  that is where the silent truncation lives.
- **Ingest safety** — a secret-shaped string and a bidirectional-override character must both be
  neutralised at write time, asserted against the store contents, not against the API response.
- **Scoping** — a second user's entries never appear, exercised through the same predicate shape
  the tasks search uses.

Gates unchanged: `task test`, `task lint`, `go vet ./...` per module, plus the frontend gates for
the UI. After any run of `go test`, `git checkout -- server/internal/db/ent/` before committing —
test runs regenerate the ent tree (`AGENTS.md:41`).

---

## 12. Deferred

| Item | Why not now |
|---|---|
| Embedding-based retrieval | Adds a model dependency and an index-refresh problem for a corpus that is small by construction |
| Automatic contradiction detection | Requires a model call in the write path of every write |
| Cross-node memory sync | Belongs with the node registry in V2 |
| Memory as a source for Obsidian pointers in both directions | The vault direction lands with the Obsidian slice; the reverse can wait until there is something worth pushing back |
| Renaming `/api/config/memory` | Its own change, with its own deprecation path |
