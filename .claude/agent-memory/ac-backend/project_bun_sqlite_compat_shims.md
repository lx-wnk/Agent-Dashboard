---
name: bun:sqlite compatibility shims in server/db/client.ts
description: Why client.ts wraps every prepared statement — bun:sqlite vs better-sqlite3 API gaps that silently break repo code
type: project
---

`server/db/client.ts` exports `getDb()` which returns a `bun:sqlite` Database wrapped with two shims installed via `installPrepareShim` → `wrapStatement`. The repo was originally written against better-sqlite3 and ALL existing repo code (every `*Repo.ts`) assumes better-sqlite3 semantics. Removing or bypassing the shim will break call sites silently.

The two gaps the shim closes:

1. **Named parameter binding** — better-sqlite3 accepts `{ slug: 'x' }` for `@slug` placeholders. bun:sqlite requires `{ '@slug': 'x' }`. `bindArgs` rewrites the object on the fly. Array bindings and objects whose keys already carry `@` / `$` / `:` are passed through.

2. **`.get()` empty-result value** — bun:sqlite returns `null`, better-sqlite3 returns `undefined`. The wrapped `.get()` normalizes `null → undefined`. This was a real bug: `taskDependenciesRepo.isBlocked` used `row !== undefined` as its empty test and therefore reported every task as blocked.

**Why:** Migration from better-sqlite3 was forced because better-sqlite3 cannot load under Bun (oven-sh/bun#4290), but rewriting every repo to use bun:sqlite-native binding shape would be a much larger churn surface. The shim is the smaller, safer change.

**How to apply:**
- When adding a new repo file or `db.prepare(...).get(...)` call, you can keep using better-sqlite3 idioms (`{slug: 'x'}`, `row === undefined`, `row ? ... : null`) — the shim handles it.
- When opening a NEW Database instance directly (e.g. tests seeding a legacy fixture), you bypass the shim. Use `@`-prefixed keys explicitly and treat `.get()` returns as `null`-or-row.
- Tests must be run via `bun test`, not `vitest`. Vitest's worker pool runs on Node where `bun:sqlite` is unresolvable. Vue/jsdom tests still use vitest; server/db/ tests use bun test.
- If considering replacing the shim with direct bun:sqlite usage everywhere, audit every `=== undefined` / `!== undefined` against `.get()` results across `server/db/` AND `server/pipeline/` AND any consumer that reads from a repo function — the bug class is silent.
