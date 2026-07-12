# ADR-0012: Plugin Domain Package Boundaries

**Status:** Proposed
**Date:** 2026-07-12

## Context

The plugin domain is spread across five `internal` packages whose names collide
and whose responsibilities overlap (finding ARCH-P2-2):

- `internal/plugin` — runtime: proxy, dispatcher, registry, auth adapter,
  manifest/validation, sanitize.
- `internal/pluginlifecycle` — discovery + lifecycle engine (`engine.go`,
  `engine_http.go`, `discovery.go`).
- `internal/pluginlifecyclectl` — a controller.
- `internal/pluginsctl` — another controller.
- `internal/pluginsettings` — settings service.

Two distinct "control plane" packages (`pluginlifecyclectl` vs `pluginsctl`)
have near-identical names and overlapping duties (listing vs lifecycle +
settings), and settings logic appears in both `pluginsettings` and
`pluginlifecyclectl`. A maintainer cannot tell from the name which package
"list plugins" lives in. The boundaries were drawn by accretion, not intent.

## Decision

Consolidate the plugin domain onto **two intentional packages** with a clear
runtime/control-plane split:

- `internal/plugin` — **runtime**: proxy, dispatch, registry, auth,
  manifest/validation. The in-request execution path.
- `internal/pluginmgmt` — **control plane**: discovery, lifecycle
  (enable/disable/install), listing, and settings. The management path invoked
  by CLI and admin API routes.

`pluginlifecycle`, `pluginlifecyclectl`, `pluginsctl`, and `pluginsettings` are
merged into `pluginmgmt`; the colliding `...ctl` names are retired. The
dependency direction is one-way: `pluginmgmt → plugin` (management operates on
the runtime registry), never the reverse.

If, during execution, a hard split within the control plane proves necessary,
the fallback is unambiguous renaming plus a follow-up ADR — but the default is
consolidation to two packages.

## Consequences

- **Discoverability.** "Where does *list plugins* live?" has one answer
  (`pluginmgmt`); "where does a plugin *request* get proxied?" has one answer
  (`plugin`).
- **No more `ctl` name collision.** `pluginlifecyclectl` / `pluginsctl` — the
  two packages a maintainer could not disambiguate — cease to exist.
- **One-way dependency.** `pluginmgmt → plugin` keeps the runtime path free of
  management concerns and independently testable; a `depguard` rule can enforce
  that `plugin` never imports `pluginmgmt`.
- **Migration cost.** A rename/merge touches all current importers of the four
  merged packages plus the API routes and CLI wiring — mechanical but broad.
  This ADR records the target; the move is a dedicated refactor.

## Alternatives Considered

1. **Keep five packages, just rename the two `...ctl` ones.** Removes the name
   collision but preserves the fragmented boundaries and the split settings
   logic. Acceptable fallback, not the goal.
2. **Collapse everything into one `plugin` package.** Rejected — mixes the
   in-request runtime path with management operations, making the runtime
   package pull in discovery/lifecycle/settings it never needs at request time.
3. **Status quo.** Rejected — accretive boundaries impose ongoing cognitive load
   and invite further mis-placement.
