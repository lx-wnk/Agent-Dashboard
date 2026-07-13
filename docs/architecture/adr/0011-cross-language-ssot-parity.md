# ADR-0011: Cross-Language SSOT Parity Enforcement

**Status:** Proposed
**Date:** 2026-07-12

## Context

`layer2-project-core.md` states the Single Source of Truth rule and then its
hard limit: *"Client and server are different languages — no cross-import."* The
Vue client (TypeScript) and the Go server cannot share a module, so any rule
that must agree on both sides is kept in parity **by hand**.

The task-slug pattern is the canonical case:

- `server/internal/validation/slug.go` — `SlugRE = ^[a-z0-9][a-z0-9-]{0,63}$`
- `src/utils/validation.ts` — `SLUG_RE` (same intended pattern)

Today the only thing linking them is a source comment
(*"mirrors src/utils/validation.ts SLUG_RE"*). Nothing fails if one side is
edited and the other is not — a validation divergence would ship silently, with
the client accepting a slug the server rejects (or vice versa) (finding
ARCH-P2-4). The SSOT table lists several such hand-synced pairs, none of which
has a drift guard.

## Decision

For every constant, regex, or validation rule that MUST agree across the Go/TS
boundary, add an automated **parity test** as the enforcement mechanism — since
a shared module is impossible, a test is the only thing that can fail on drift.

Adopt the slug-parity test as the template:

- A Vitest test reads the Go source literal from disk (e.g.
  `server/internal/validation/slug.go`), extracts the pattern, and asserts it
  equals the TypeScript `SLUG_RE.source`.
- The test also runs a shared set of accept/reject inputs against **both**
  patterns, so a *semantic* divergence fails even if the literals were
  refactored differently.
- The test fails loudly (not skips) if the referenced source file is missing or
  moved.

Each hand-synced cross-language pair in the SSOT table gets one such test.
New cross-language shared rules MUST land with their parity test in the same
change.

## Consequences

- **Drift becomes a red test, not a production bug.** Editing one side without
  the other fails CI (`pnpm test`).
- **Reusable template.** One parity test per pair; the pattern generalizes to
  any Go↔TS shared literal (status enums, model lists, message strings).
- **Cross-language coupling by path.** The test reads a sibling-language source
  file by repo-relative path — a moved file surfaces as a failing test, which is
  the intended safety behaviour.
- **No runtime change.** Enforcement lives entirely in the test suite; shipping
  code is untouched.

## Alternatives Considered

1. **Code-generate one side from the other.** A build step could emit the TS
   constant from the Go source (or a shared JSON). Heavier tooling, a new build
   dependency, and generated files to review — disproportionate for a handful of
   small constants. Rejected for now; revisit if the number of shared rules
   grows large.
2. **Rely on the linking comment + review.** The status quo. Rejected — comments
   do not fail builds; the divergence already went unguarded.
3. **Shared module.** Impossible — Go cannot import TypeScript and vice versa.
