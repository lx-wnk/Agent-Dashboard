# Plugin Domain Consolidation + OAuthKit — Design Spec

> Date: 2026-07-12 · Status: Approved · Branch: `docs/audit-spec-roadmap` (off `upcoming`)
> Audit follow-up from `outputs/Findings-full-project-2026-07-12.md`: ARCH-P2-2, CQ-08.
> Ships AFTER `2026-07-12-spawn-channel-security-hardening-design.md` — this is a large, mechanical package
> move; sequencing it second keeps the security diffs from rebasing through the rename.

## Why

Two maintainability findings. No exploit path — pure structural debt that raises the cost and error rate of
every future plugin/auth change.

- **ARCH-P2-2** — the plugin domain is fragmented across **5 packages** (`server/internal/plugin`,
  `pluginlifecycle`, `pluginlifecyclectl`, `pluginsctl`, `pluginsettings`) with colliding controller names
  `pluginsctl` / `pluginlifecyclectl`. The runtime (registry, subprocess supervision, dispatch) and the
  control plane (HTTP handlers, lifecycle commands, settings delivery) are interleaved, so a reader cannot
  tell which package owns process state vs. which owns request handling.
- **CQ-08** — `github-oauth` and `office365-oauth` plugins copy-paste the entire OAuth/session/CSRF plumbing
  (authorize redirect, state/nonce, token exchange, session cookie mint/verify, CSRF check). Two divergent
  copies of security-critical code; a fix to one silently skips the other.

## Decisions (user-approved)

| # | Decision | Rationale |
|---|---|---|
| D1 | Consolidate the 5 plugin packages into **two**: `plugin` (runtime) + `pluginmgmt` (control plane). | Clear seam: runtime owns process/registry state; `pluginmgmt` owns HTTP + lifecycle commands + settings delivery. Kills the `pluginsctl`/`pluginlifecyclectl` name collision. |
| D2 | Extract the shared OAuth/session/CSRF plumbing into a **`oauthkit` Go module**; both OAuth plugins depend on it. | One audited implementation of security-critical flow; provider-specifics (endpoints, scopes, claims) stay in each plugin as config/adapters. |
| D3 | **Golden tests captured on `upcoming` BEFORE the move**, replayed after, to prove no behavioral drift. | A package move / de-dup of CSRF and session code must be provably behavior-preserving; golden vectors catch silent divergence. |
| D4 | This spec ships **after** the security spec; it is a refactor-only change behind a full green build + suite. | Prevents the small, high-value security diffs from having to rebase through a large rename. |

## Scope

In: merge `pluginlifecycle` + `pluginlifecyclectl` + `pluginsctl` + `pluginsettings` into `plugin` (runtime)
and a new `pluginmgmt` (control plane); update all imports, DI wiring (`server/cmd/serve/di_*.go`), and router
registrations; extract `oauthkit`; refactor both OAuth plugins onto it; golden tests before/after.

Out: any functional change to plugin behavior, the subprocess/proxy protocol, or the OAuth flow semantics;
new providers; UI changes beyond import-path fixes; the security items in the sibling spec.

## Architecture (file-anchored)

### Target package layout

- **`server/internal/plugin`** (runtime): `Registry`, `Entry`, subprocess supervision (`startEntry`,
  `watchPlugin`, `gracefulStop`, `StartOne`/`StopOne`/`Shutdown`), capability lookup, dispatch, and the
  secret-env strip-set (see the security spec's CQ-02 — `dashboardSecretEnv` = shared base + `MCP_TOKEN`
  stays here). Absorbs `pluginlifecycle` (process lifecycle primitives) and `pluginsettings` (settings-env
  assembly consumed by `appendSettingsEnv`).
- **`server/internal/pluginmgmt`** (control plane): HTTP handlers, the lifecycle command surface currently in
  `pluginlifecyclectl`, the management controller currently in `pluginsctl`, and settings-delivery endpoints.
  Depends on `plugin`; `plugin` must **not** depend on `pluginmgmt` (one-way edge — enforce by build).

### Migration order (mechanical, compiler-guided)

1. Land the sibling security spec first (CQ-18 `StopOne` fix lives in `plugin/registry.go` — do it there
   before the move so the race fix isn't entangled with rename churn).
2. Fold `pluginlifecycle` + `pluginsettings` into `plugin`; fix imports; `go build ./...`.
3. Create `pluginmgmt`; move `pluginlifecyclectl` + `pluginsctl` into it under non-colliding symbol names;
   fix imports and router/DI registration; `go build ./...`.
4. Re-run the full suite under `-race`; diff the router's registered routes before/after (must be identical).

### `oauthkit` (`server/internal/oauthkit` or a module under the plugins tree)

- Provider-agnostic core: authorize-URL builder (state + PKCE/nonce), state store + verify, token exchange,
  session cookie mint/verify (reuse the existing signing/`secretbox` primitives — do not fork them), and the
  CSRF check applied on the callback + session endpoints.
- Per-provider adapter interface: `AuthorizeEndpoint`, `TokenEndpoint`, `Scopes`, `UserInfo`/claims mapping,
  cookie/session naming. `github-oauth` and `office365-oauth` each provide one adapter and delegate all flow
  logic to `oauthkit`.
- Reference the existing office365 design (`docs/superpowers/specs/2026-05-19-office365-sso-plugin-design.md`)
  and the permission-ingress bearer-auth spec to keep session/CSRF semantics identical.

## Data flow

Unchanged at runtime. Post-consolidation: an inbound plugin management request enters `pluginmgmt` (HTTP) →
calls into `plugin` (runtime) for registry/process actions → returns. OAuth: browser → provider plugin
handler → `oauthkit` (authorize/callback/session/CSRF) → provider adapter for endpoint + claims. The route
table, wire formats, cookies, and headers are byte-identical before and after (D3 proves it).

## Error handling

- Refactor-only: no new error paths. Preserve every existing error string, HTTP status, and log line that is
  externally observable (tests and the UI may match on them). Where a symbol is renamed, keep the emitted
  message text the same.
- `oauthkit`: the shared CSRF/state-mismatch and token-exchange-failure paths must return the **same** status
  codes and bodies the two plugins return today (captured as golden vectors).

## Testing

- **Golden vectors (D3):** on `upcoming`, before any move, capture: full router route list; OAuth
  authorize-redirect (URL + state/PKCE shape), callback success + CSRF-failure + state-mismatch responses,
  and the minted session cookie structure for both providers. Store as fixtures.
- After the move + extraction, replay the vectors: routes identical; OAuth responses byte-identical (modulo
  intentionally random state/nonce, which is asserted structurally). Any diff blocks the change.
- Import-cycle guard: assert `plugin` does not import `pluginmgmt` (test or `go list` check).
- Full `go build ./...` + `go test ./... -race`; `pnpm lint && pnpm typecheck && pnpm test` green before commit.

## Risks

- **Silent CSRF/session divergence in `oauthkit`** — the highest-risk item; de-duping security-critical code
  can subtly change behavior. Mitigated by D3 golden vectors captured before the move and replayed after.
- **Merge-conflict surface** — a 5→2 package move touches DI wiring and many import paths; landing it while
  other plugin work is in flight invites conflicts. Mitigated by D4 (ship after the security spec, on a green
  base) and the compiler-guided step order.
- **Name-collision fallout** — `pluginsctl`/`pluginlifecyclectl` symbols moving into `pluginmgmt` under new
  names can break references in DI/router; the `go build ./...` gate after each step catches these early.
- **Hidden runtime→control-plane coupling** — if `plugin` currently reaches into a controller, the one-way
  edge (D1) forces an interface seam; surface it during step 3 rather than leaving a cycle.
