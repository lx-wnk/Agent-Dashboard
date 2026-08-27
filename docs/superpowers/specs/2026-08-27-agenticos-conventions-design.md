# AgenticOS — Conventions and Normalization

**Date:** 2026-08-27
**Status:** Approved design
**Stage:** MVP, carried by the units that touch each area
**Parent:** `2026-08-27-agenticos-overview-design.md`

---

## 1. Purpose

The existing code grew historically. ARMS pulls five working mechanisms together, and pulling
things together only pays off if they agree on vocabulary. This document is the agreement.

It exists because renaming and re-scoping existing constructs is explicitly permitted, and because
the project's stated principles — **DRY, KISS, Clean Code, spec-driven development, single source
of truth, object orientation** — make several current constructs defects rather than quirks.

**It is not a refactoring backlog.** Every item here is carried by the unit that already touches
that code. Nothing in this document is done for its own sake.

---

## 2. The principles, made concrete for this codebase

| Principle | What it means here |
|---|---|
| **DRY** | A rule, regex, constant or path template exists once. A second copy is a defect even when both copies are currently correct — they will diverge |
| **KISS** | The smallest construct that holds. No generic repository, no abstraction over ent, no framework for four resource kinds |
| **Clean Code** | Named input structs over positional parameters; no boolean flag that changes what a function means; a function name that survives reading its body |
| **Spec-driven** | The spec lands before the code, and the code is checked against it. These seven documents are that contract |
| **SSOT** | Already a written project rule with a canonical-locations table. This document extends that table rather than competing with it |
| **OOP** | In Go: small interfaces at the seams, one type owning one responsibility, behaviour with its data. Not inheritance hierarchies |

---

## 3. Duplications to collapse

Each is a concrete finding with evidence, and each names the unit that carries it.

### C1 — The slug rule exists three times · carried by K1

- Canonical: `validation.SlugRE` (`server/internal/validation/slug.go:10`), which says in its own
  comment "Import this instead of defining a local copy."
- Copy: `pluginIDRe` (`server/internal/plugin/registry.go:23-25`) — same shape, **unbounded
  length**.
- Copy: four handler sites hardcode `"slug must match ^[a-z0-9][a-z0-9-]{0,63}$"` instead of using
  the exported `validation.SlugPatternMessage` (`api/spawners/handler.go:168,253`,
  `api/projects/handler.go:251,314`).

`pluginIDRe` is deleted; the message constant is used. Note this is a **behaviour change**: an
over-long plugin id that was accepted is now rejected, and that gets its own test.

ADR-0011 already names this pair as the canonical cross-language SSOT case and is still marked
*Proposed*. It moves to *Accepted* with this work, since the parity test it mandates gets written.

### C2 — Two sanitizers that do not share code · carried by K3

`server/internal/sanitize/sanitize.go` filters by Unicode category (`Cf` covers bidi controls,
plus `IsControl` for C0/C1) and counts truncation without marking it — for a stated reason
(`sanitize.go:32-35`): "A marker inside the text is one the text can forge."

`server/internal/cmdscope/sanitize.go` does the same job with an explicit codepoint-range switch
and **appends an ellipsis**. `cmdscope` does not import `sanitize`.

One implementation, in `sanitize`, with the counted-not-marked rule. Its package doc already
records why the second boundary was missed the first time; a third boundary — memory — is exactly
when that lesson gets tested again.

### C3 — Two tilde expanders and a duplicated env-override block · carried by K4

`pathutil.ExpandLeadingTilde` (`pathutil/pathutil.go:10-24`) handles bare `~` and `~/`.
`provider/registry.go:319-324` has its own `expandHome` handling only `~/`. The env-override block
that resolves a provider's config dir is copy-pasted between `ConfigDirs()`
(`registry.go:193-197`) and `KnownProviders()` (`registry.go:144-149`).

`provider` imports `pathutil`; the override block becomes one function. The materializer depends on
config-dir resolution being right, so this lands with it.

### C4 — Repository boilerplate · carried by K1

27 hand-written repositories with identical shape, two transaction idioms, and the only two shared
functions living inside unrelated files: `IsNotFound` in `repo/plugin_repo.go:32` and `rollback` in
`repo/spawner_repo.go:158-163`.

One shared file with `IsNotFound`, `rollback` and a single `WithTx`. No base type, no generics —
see the registry spec §3.5 for why that boundary and not a wider one.

### C5 — Positional constructors with clear-flags · carried by K1

`SpawnerRepo.Create`/`Update` take 11 parameters, `ProjectRepo.Create`/`Update` take 12, both with
`clearX bool` arguments to distinguish "unchanged" from "set to NULL"
(`repo/project_repo.go:127-146`). Every other repo above four parameters already uses a named input
struct.

Input structs, with a nillable pointer expressing "clear" — one representation instead of two
parallel arguments that can disagree.

### C6 — The FTS query sanitizer · carried by K3

`sanitizeFtsQuery` (`api/search/handler.go:160-171`) is the only FTS input normalizer. Memory
retrieval needs the same rule. It moves to a shared location **before** the second caller exists,
rather than after — which is the only moment when moving it is free.

---

## 4. Vocabulary collisions to resolve

### V1 — "memory" means two things · carried by K3

`/api/config/memory` enumerates `CLAUDE.md` and `AGENTS.md` files for the config explorer
(`api/config/handler.go:46-53,117-125`), and `/memory` is a built-in slash command name
(`cmdscope/enumerate.go:95`). The Memory resource is something else entirely.

Since renaming is permitted: the config-explorer concept becomes **context files** —
`/api/config/context-files`, `ContextFileEntry`, "Context files" in the UI. That name is also more
accurate than "memory" ever was for a list of markdown files on disk.

The endpoint rename ships with a deprecation window: the old path answers and logs for one minor
version.

### V2 — "drift" means model quality · carried by K4

`drift_alert` means model-quality drift per `(spawner_id, model, stage, metric_key)`
(`schema/drift_alert.go:14-40`). The materializer's concept is `materialization_conflict`.

**`drift_alert` is not renamed.** The name is accurate for what it does; renaming a working table
to free a word we have already agreed to avoid is churn, not clarity.

### V3 — "permission" means four things · carried by K2

The static allow-list, the DB round trip, the hook bridge and the MCP scope model all use the word.
After K2 there is one vocabulary: **capability** (what may be done), **grant** (permission bound to
a context), **decision** (the answer), **enforcer** (the thing that acts on it).

Concrete renames: `PermissionBridge` → `HookEnforcer`, and the `permissions` package becomes the
policy package that owns the capability catalogue.

---

## 5. Dead and misleading constructs to remove

| Construct | Evidence | Action | Unit |
|---|---|---|---|
| `task_permission.pre_approved` | four writers, zero readers | drop | K2 |
| `task_permission.decided_by` / `decided_at` | never written outside generated ent | wire, do not drop — identity on a decision is required | K2 |
| `permissions.WriteToolNames` | exported slice, zero non-test readers; `IsWriteTool` is the real SSOT | drop | K2 |
| `system_prompt.scope = "task"` | declared in the schema, rejected by the handler, never wired | drop the value, or wire it. It is dropped — Memory now covers per-task context properly | K3 |
| `skills-lock.json` and its install snippet | nothing reads the file; the documented `jq` command reads a `.name` key that does not exist and treats an `owner/repo` slug as a URL; the doc's skill table contradicts the file's contents | replaced by registry `origin`/`origin_ref`; file and snippet removed together | K4 |
| `provider.EnabledFunc` comment | names `DASHBOARD_PROVIDERS_ENABLED` as the source, but that key moved to DB-backed settings (`config/config.go:190` lists it in `movedKeys`) | correct the comment | K4 |

---

## 6. Traps that must not be repeated

These are properties of the current code that new work has to respect. They are listed here so no
spec has to rediscover them.

**T1 — A new MCP scope must be added in two places.** `ToolScopeMap` (`mcp/auth.go:18-50`) gates the
call, but `validKeyScopes` (`mcp/tools/keys.go:17-21`) gates whether a key can be *granted* the
scope — and it omits `agent:coord`, which is therefore reachable only through implication. A scope
added to one and not the other is either ungrantable or ungated.

**T2 — `ResolveScopes` is one level deep.** `scopeImplies` (`auth.go:52-58`) is expanded once, not
transitively. It is complete today only because `keys:manage` enumerates everything explicitly. A
new scope inserted in the middle silently breaks the closure.

**T3 — Registering an MCP tool without a scope entry panics at construction.**
`mcp/registry.go:51-61` panics on a duplicate name or a missing `ToolScopeMap` entry, deliberately,
because the alternative failure (`requiredScope == ""`, which no key holds) is a permanent silent
denial.

**T4 — ent index changes rebuild SQLite tables and crash on existing databases.** Pre-create the
index under ent's exact generated name, read from `db/ent/migrate/schema.go`
(`db/client.go:429-435`, PR #207).

**T5 — A JSON column added to a populated table needs a raw `entsql.Default("{}")`.** The escaped
form ships literal `''{}''` and crashes every load. It happened once; the repair migration is still
in the tree (`serverapp/plugin_migrate.go:21-41`).

**T6 — `go test` regenerates the ent tree.** `git checkout -- server/internal/db/ent/` before
committing unless the regeneration is the change (`AGENTS.md:41`).

**T7 — Web push looks wired and is not.** `webpush.Service.SendToAll` (`webpush/service.go:89`) has
zero production callers. Any spec claiming push notification of a pending decision is wrong until
that changes.

---

## 7. Standards this work establishes

**Writing a file outside the repo.** Temp file in the target directory → `Sync` → `Close` →
`Chmod` → `Rename`. Only three writers do this today
(`hookscript.go:47-72`, `cmd/serve/hooks.go:213-250`, `api/config/file.go:190-209`), and only the
first two call `Sync`. That is the standard; everything else uses plain `os.WriteFile` and should
not.

**Owning a file the user can also edit.** The `cmd/serve/hooks.go` model: refuse rather than
overwrite on a parse failure; a marker is not proof of ownership, the path is; foreign entries are
surfaced, never deleted; the outcome is three-way (`unchanged | installed | repaired`), not a
boolean.

**Never exposing secrets in a DTO.** `PluginView` omits addr, command and env deliberately
(`api/plugins/handler.go:16-18`). Every new resource DTO states in a comment what it omits and why.

**Failing closed, and saying so.** Every gate declares its posture. The four postures currently in
the codebase — fail-open hook bridge, fail-closed edit gate, fail-closed ACP gate, silent-drop
spawn filter — stay as they are, but become declared properties rather than implicit behaviour.

**Truncation is counted, not marked.** Everywhere, per `sanitize.go:32-35`.

---

## 8. Deliberately unchanged

Listing these matters as much as listing the changes, because "we may rename anything" is an
invitation to churn.

| Construct | Why it stays |
|---|---|
| `spawner` | Accurate, load-bearing, and its resolution chain is the model the rest of the design copies |
| `drift_alert` | Accurate for what it does. We avoid the word elsewhere instead |
| 27 repository interfaces | The duplication that hurts is transactions and constructors, not the interfaces. KISS |
| Error wrapping `fmt.Errorf("entity.Method: %w", err)` | Already uniform across the layer |
| Dual persistence (filesystem-derived monitoring, SQLite pipeline) | An accepted ADR, and Memory does not cross it |
| Auto-migrate plus hand-written pre-migrations | Versioned migrations are a known gap and a separate decision |
| The static Bash allow-list's conservatism | Crude, but it fails safe. Replacing it is a security change deserving its own spec |
