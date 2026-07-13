# Spawn / Channel Security Hardening — Design Spec

> Date: 2026-07-12 · Status: Approved · Branch: `docs/audit-spec-roadmap` (off `upcoming`)
> Audit follow-up from `outputs/Findings-full-project-2026-07-12.md`: CQ-02, CQ-09, SEC-P3-003, CQ-18.
> Ships FIRST — small, high-value, no structural churn. The plugin-domain consolidation (ARCH-P2-2 / CQ-08)
> ships in a separate spec AFTER this one so these security diffs don't rebase through a package move.

## Why

Four independent local-first hardening items on the agent-spawn and channel paths. Threat model: binds
`127.0.0.1`, reads sensitive `~/.claude` session data, spawns real Claude agents and shells; in optional
GitHub-OAuth multi-user mode a non-admin authenticated user is a realistic attacker.

- **CQ-02** — three divergent secret-env blocklists. Interactive/pipeline spawn strips only 2 keys
  (`DASHBOARD_JWT_SECRET`, `DASHBOARD_HOOKS_SECRET`), so the server's `DASHBOARD_SECRET_KEY` (plugin master
  key) and `DASHBOARD_AUTH_PLUGIN_SECRET` (auth-bypass secret) **leak into every spawned Claude agent's env**.
  The plugin path strips 5. No single canonical set exists.
- **CQ-09** — `channel/dashboard-channel.ts:404` `POST /message` forwards its body into Claude as an
  instruction with **no auth check**, even though a per-process discovery token already gates every outbound
  call and both Go re-implementations (`bridge.go:231`, `ptyhost.go:134`) enforce it. Instruction-injection
  gate is missing on the one live TS bridge.
- **SEC-P3-003** — `api/agents/terminal.go` pty WebSocket relies solely on the `websocket.Accept` same-origin
  check when `auth.mode=none`, and calls `SetReadLimit(-1)` (unbounded) on both browser and broker conns.
- **CQ-18** — `plugin/registry.go:293` `StopOne` copies the `Entry` under lock, unlocks, then acts on the
  stale copy's `cmd`; a concurrent `watchPlugin` restart can leave the restarted process running.

## Decisions (user-approved)

| # | Decision | Rationale |
|---|---|---|
| D1 | **CQ-02 uses a 4-key shared base, NOT the audit's literal single unified set.** `DeniedSecretEnvKeys = {DASHBOARD_SECRET_KEY, DASHBOARD_JWT_SECRET, DASHBOARD_AUTH_PLUGIN_SECRET, DASHBOARD_HOOKS_SECRET}` — pure server secrets never needed downstream, stripped in **both** spawn paths and the plugin path. | These four are consumed only by the server (`secretbox.go`, JWT signer, `config.go` auth bypass, hooks HMAC). No spawned agent or plugin has any legitimate use for them. |
| D2 | **`DASHBOARD_MCP_TOKEN` is handled per-consumer — it stays OUT of the shared base.** The pipeline spawner mints a **per-task** MCP token and injects it at Stage 3 (`spawner.go:345`); the plugin path adds `MCP_TOKEN` to its own strip-set on top of the base. | A single flat set consumed by the spawner's final-pass loop (`spawner.go:354`) would **delete the just-injected per-task token** and break the channel bridge's `/api/mcp` callback (`bridge.go:55` reads `os.Getenv("DASHBOARD_MCP_TOKEN")`). Plugins get no token, so the plugin path strips it. |
| D3 | **CQ-09 is a patch, not a delete** — `channel/dashboard-channel.ts` is a live, documented MCP server (own package). Add a Bearer check to `POST /message` with a constant-time compare, 401 on mismatch. | The Go caller `sendHTTPMessage` (`spawn.go:751`) **already always sends** `Authorization: Bearer <token>`; both Go bridges already enforce it. Pure parity gap — safe to enforce, no caller breaks. |
| D4 | **SEC-P3-003: add defense-in-depth, keep same-origin.** Layer loopback + CSRF/auth on the terminal route (not just `websocket.Accept`), and cap `SetReadLimit` to a finite ~1 MiB bound on both conns. | Same-origin is the only current control under `auth.mode=none`; a finite read cap removes an unbounded-frame memory-DoS. |

## Scope

In: one canonical `DeniedSecretEnvKeys` set (Go) consumed by all three strip sites; the per-consumer
`MCP_TOKEN` delta documented in code; a Bearer check on the TS `/message` handler; terminal-WS auth
defense-in-depth + finite read cap; a race-free `StopOne`; a spawn test asserting a live pipeline agent still
reaches `/api/mcp` after the CQ-02 change.

Out: the plugin-package consolidation and `oauthkit` extraction (separate spec, ships after); rewriting the
channel transport; changing the discovery-token rotation model; SEC-INFO-005/006 (git home-confine, global
loopback rate-limiter) — informational, may be folded in opportunistically but not required here.

## Architecture (file-anchored)

### CQ-02 — canonical deny-set (`server/internal/spawn/` + `server/internal/pipeline/spawner.go` + `server/internal/plugin/registry.go`)

- Introduce `DeniedSecretEnvKeys` (a `map[string]struct{}` of the 4 D1 keys) in one canonical Go location
  (e.g. a small `server/internal/secretenv/secretenv.go`, or promote the existing `spawn` var). Per project
  SSOT rule the value lives in exactly one place and is imported — never re-declared.
- `pipeline/spawner.go:272` `deniedEnvKeys` → replace the 2-key literal with the shared 4-key set. The
  Stage-1 (spawner-env, `:308`) and Stage-2 (inherited-env, `:326`) filters and the **final defense-in-depth
  pass** (`:354`) all consume it. Because `DASHBOARD_MCP_TOKEN` is deliberately **excluded** from the base,
  the Stage-3 injection at `:345` survives the `:354` delete loop — this is the load-bearing per-consumer
  delta. Add a one-line comment at `:354` naming why `MCP_TOKEN` is not in the set.
- `spawn/spawn.go:992` `deniedEnvKeys` (interactive spawn) → replace the 2-key literal with the same shared
  set. Interactive spawns get no `MCP_TOKEN` at all, so no exclusion nuance applies here — the 4 keys are
  simply stripped from inherited env.
- `plugin/registry.go:690` `dashboardSecretEnv` → redefine as `base ∪ {DASHBOARD_MCP_TOKEN}` (import the
  shared base, add the one extra key locally). Result is the existing 5-key behavior, now expressed as
  base + delta rather than a hand-maintained duplicate.

### CQ-09 — Bearer check on TS `/message` (`channel/dashboard-channel.ts`)

- The per-process token already exists: `TOKEN = randomBytes(16).toString('hex')` (`:58`), written into the
  discovery file (`:474`) and sent on every outbound call (`:206`, `:268`, `:283`, `:342`).
- In the `POST /message` handler (`:404`), before parsing the body: read `req.headers.authorization`, require
  exactly `Bearer <TOKEN>`, compare with a **constant-time** equality (`crypto.timingSafeEqual` over equal-
  length buffers; length-mismatch → fail without comparing). On any mismatch/absence return `401` with
  `{"error":"unauthorized"}` and do **not** forward the notification. Mirror the Go pattern
  (`bridge.go:231` `token.authorize(r)` → 401).

### SEC-P3-003 — terminal-WS hardening (`server/internal/api/agents/terminal.go`)

- Keep the documented same-origin check at `websocket.Accept`. Add defense-in-depth so the route is not
  same-origin-only under `auth.mode=none`: gate the handler behind the loopback + CSRF/auth posture used by
  the other `/api/agents/{pid}/*` mutations (the route is a `GET` upgrade, so it is currently outside
  `RequireSameOriginForMutations` — wire an equivalent explicit check here).
- Replace `SetReadLimit(-1)` at `:102` (browser conn) and `:115` (broker conn) with a finite cap
  (`terminalReadLimitBytes = 1 << 20`, ~1 MiB). Apply the same finite cap at `ptyhost.go:214` for symmetry so
  the broker side is bounded too.

### CQ-18 — race-free StopOne (`server/internal/plugin/registry.go:293`)

- Root cause: `StopOne` copies `e := r.plugins[i]` under lock (`:299`), unlocks (`:304`), then calls
  `gracefulStop(target.cmd, target.cmdDone)` on the **stale copy**. Between the copy and the stop, a
  concurrent `watchPlugin` restart (`:528`) can replace the live `cmd`, so the restarted process is never
  signalled and keeps running.
- Fix: call `setIntentionalStop(id)` **before** reading the entry so the watcher will not respawn during the
  stop window, then re-read the current `cmd`/`cmdDone` and perform `gracefulStop` while holding the lock (or
  under a short re-check that the generation/cmd has not changed). Ensure `removeByID` and the stop act on the
  same generation. No behavior change for the no-op (absent entry) path.

## Data flow

1. **Spawn (pipeline):** orchestrator → `BuildSpawnEnv` → Stage-1/2 filter by `DeniedSecretEnvKeys` → Stage-3
   injects per-task `DASHBOARD_MCP_TOKEN` → final `:354` delete loop strips the 4 base keys (MCP token
   survives) → agent env. Agent's in-process channel bridge reads `DASHBOARD_MCP_TOKEN` and calls
   `/api/mcp`. Post-fix, `SECRET_KEY` / `AUTH_PLUGIN_SECRET` no longer appear in that env.
2. **Channel inbound:** dashboard `sendHTTPMessage` (`spawn.go:751`, always `Bearer <disc.Token>`) → TS
   `POST /message` → **new** constant-time Bearer check → MCP notification into Claude. Unauthenticated
   callers now get 401 before injection.
3. **Terminal:** browser `GET /api/agents/{pid}/terminal` → same-origin + **new** loopback/CSRF gate →
   `websocket.Accept` → bidirectional pump with a finite per-frame read cap on both hops.

## Error handling

- CQ-02: stripping is silent by design (secrets simply absent). No new error path. The final-pass loop is the
  invariant guard; the comment documents the `MCP_TOKEN` exception so a future edit doesn't "fix" it into the set.
- CQ-09: missing/short/mismatched token → `401`, body not forwarded, no partial notification. Length-mismatch
  must short-circuit before `timingSafeEqual` (which throws on unequal-length buffers).
- SEC-P3-003: a frame exceeding the read cap closes the WS with a protocol error (nhooyr/coder-websocket
  behavior) rather than allocating unboundedly. Existing `Accept`-failure handling unchanged.
- CQ-18: absent entry stays a no-op returning `nil`. Stop failures propagate as today.

## Testing

- **CQ-02 (the required assertion):** a spawn/integration test that builds a pipeline agent env via
  `BuildSpawnEnv` with a per-task `MCPToken` set, then asserts (a) none of the 4 base keys appear in the
  resulting env, and (b) `DASHBOARD_MCP_TOKEN` **is** present with the injected value — i.e. a live agent can
  still reach `/api/mcp`. Add a parallel interactive-spawn test asserting the 4 keys are stripped there too,
  and a plugin-env test asserting all 5 (base + `MCP_TOKEN`) are stripped.
- **CQ-09:** TS unit test on the `/message` handler — valid Bearer → 200 + notification fired; missing header,
  wrong token, and correct-length-wrong-value → 401 + no notification. Include a same-length wrong token to
  exercise the constant-time path.
- **SEC-P3-003:** handler test that a non-loopback / bad-origin upgrade is rejected under `auth.mode=none`; a
  unit assertion that both conns are configured with the finite limit (not `-1`).
- **CQ-18:** concurrency test — start a plugin, race a simulated `watchPlugin` restart against `StopOne`,
  assert no process survives and the registry entry is removed. Run under `-race`.
- Gate: `go build ./...` + `go test ./... -race` green; `pnpm lint && pnpm typecheck && pnpm test` green
  before any commit.

## Risks

- **CQ-02 breaking spawned agents** — if `MCP_TOKEN` were accidentally pulled into the base, the pipeline
  agent loses `/api/mcp` access. Mitigated by D2 exclusion + the mandatory spawn test (a) & (b) above and the
  `:354` comment.
- **CQ-09 breaking the channel** — ruled out: the caller already sends the token (`spawn.go:751`). Only
  residual risk is a second, undocumented caller of `/message` without a token — grep confirms the Go
  dashboard path is the sole caller; verify no external tooling posts to it before enforcing.
- **SEC-P3-003 over-tightening** — adding auth on the terminal route must not lock out the legitimate
  same-origin browser under `auth.mode=none`; the added check is loopback/CSRF, not a bearer requirement, so
  the local UI keeps working.
- **CQ-18 fix widening the lock** — holding the mutex across `gracefulStop` could serialize stops; keep the
  critical section minimal (mark intentional + snapshot current cmd under lock, signal outside if needed) and
  cover with the `-race` test.
