# ADR-0004: Stdlib-Only Domain Error Sentinels

**Status:** Accepted
**Date:** 2026-06-09

## Context

The db layer (`server/internal/db/repo/system_prompt_repo.go`) wraps not-found
results with a shared sentinel so that callers higher up the stack can map them
to the correct HTTP status via `errors.Is`. It imported that sentinel from
`server/internal/apierr`:

```go
return nil, fmt.Errorf("%w: system prompt %s", apierr.ErrNotFound, id)
```

But `apierr` also owns the HTTP-mapping middleware (`ErrorMiddleware`,
`AppError`, `WriteJSON`) and therefore imports `net/http`, `encoding/json`, and
`log/slog`. Importing `apierr` from `db/repo` dragged the entire HTTP stack into
the database layer — a transitive dependency the db layer has no business
carrying (Arch-P3-1). It also inverts the intended layer direction: the db layer
is a low-level leaf and must not depend on a presentation-layer package.

The four sentinels (`ErrNotFound`, `ErrConflict`, `ErrBadRequest`,
`ErrForbidden`) are pure domain vocabulary. Their HTTP status mapping is a
separate concern that legitimately belongs in `apierr`.

## Decision

Extract the sentinels into a new stdlib-only leaf package
`server/internal/domainerr` that imports **only** `errors`:

```go
package domainerr

import "errors"

var (
	ErrNotFound   = errors.New("not found")
	ErrConflict   = errors.New("conflict")
	ErrBadRequest = errors.New("bad request")
	ErrForbidden  = errors.New("forbidden")
)
```

`apierr` re-exports them as aliases so every existing consumer keeps working
unchanged:

```go
var (
	ErrNotFound   = domainerr.ErrNotFound
	ErrConflict   = domainerr.ErrConflict
	ErrBadRequest = domainerr.ErrBadRequest
	ErrForbidden  = domainerr.ErrForbidden
)
```

Go alias identity preserves `errors.Is` matching: the alias and the original are
the same `*errorString` pointer, so a `domainerr.ErrNotFound` wrapped in the db
layer still satisfies `errors.Is(err, apierr.ErrNotFound)` inside
`apierr.ErrorMiddleware`. The db layer is repointed to `domainerr`; no other
consumer changes.

`apierr` remains the single site that maps these sentinels to HTTP status codes.
The error string values are identical to the previous `apierr` values, so HTTP
response bodies are byte-for-byte unchanged.

## Consequences

- **db layer no longer imports `net/http`.** `db/repo` depends on `domainerr`
  (stdlib-only), restoring the intended layer direction. Verified with
  `grep -rl 'internal/apierr' server/internal/db/` returning empty.
- **No behaviour change.** Sentinel string values, `errors.Is` semantics, and
  HTTP status mapping are all preserved by alias identity.
- **Single source of truth.** The sentinels live in exactly one place
  (`domainerr`); `apierr` aliases rather than redefines them.
- **Future guard (optional).** A `depguard` lint rule could enforce that
  `server/internal/db/...` never imports `internal/apierr`, making the severed
  dependency a compile-time-checked invariant rather than a convention. Deferred
  as a follow-up.

## Alternatives Considered

1. **Duplicate the sentinels in the db layer.** Rejected — violates SSOT; two
   `ErrNotFound` values would not satisfy `errors.Is` across layers.
2. **Move `ErrorMiddleware` out of `apierr` instead.** Larger blast radius (all
   28 `apierr.*` consumers) for no additional benefit; the sentinel/mapping split
   is the minimal cut.
