# Security

The dashboard reads sensitive Claude session data from your machine. It is designed local-first and defensive by default.

- **Loopback only** — the server binds to `127.0.0.1` and is never exposed to the network. (Multi-machine mode is opt-in and expects a VPN/SSH tunnel — see [Configuration](configuration.md).)
- **Local-trust auth bypass** — when on loopback with no GitHub OAuth configured, all API requests are allowed without login. This is safe for a single-user developer machine. Any process with access to `127.0.0.1:13120` can create API keys, spawn agents, and read session data — so for shared or multi-user machines, configure GitHub OAuth (`DASHBOARD_GITHUB_CLIENT_ID` + `DASHBOARD_GITHUB_CLIENT_SECRET`).
- **Ephemeral JWT secret** — `DASHBOARD_JWT_SECRET` is auto-generated if unset (sessions reset on restart). Set a stable value for production.
- **Hashed tokens** — bearer tokens are SHA-256 hashed before storage; raw tokens are shown once and never persisted in plaintext.
- **Authenticated channel replies** — per-agent bearer tokens authenticate channel replies.
- **Sanitized output** — markdown is sanitized via DOMPurify before any `v-html` rendering.
- **Rate-limited spawns** — user-initiated spawns are rate-limited (default 5/min, configurable).
- **Dangerous-command block-list** — a block-list in the spawner rejects `curl`/`wget`/`eval`/shell-substitution in agent tool grants.
- **`git push` hard-blocked** by default even when granted; opt out with `DASHBOARD_ALLOW_GIT_PUSH=true` or per-task `metadata.allowGitPush=true`.

See also the [Privacy policy](../../PRIVACY.md).

## Capabilities and the permission gate

A **capability** is a named permission coarser than a raw tool name — `Bash`,
`WebFetch`, or a future action like `mail.send` that has no Claude Code tool
behind it at all. Claude Code's own permission system can only grant or deny
a *tool*; it has no unit for "this agent may reach the network" or "this
agent may spend up to $5", so those questions had nowhere to live. Every
capability also carries a **class** (`tool`, `reach`, `resource`, `spend`)
that decides its default when nothing has granted or denied it explicitly:
`tool`/`reach`/`resource` default to asking, `spend` and any unrecognised
class default to deny.

One pure **Decider** (`server/internal/capability`) resolves a capability
request to allow / deny / ask by ranking the grants that apply to it: the
most specific matching context wins (agent session, then task, then routine,
then application, then project, then global), and within one context level a
deny beats an allow beats an ask. Grants are rows with an optional expiry and
rate limit, bound to that context. A capability also declares which
enforcement points it can be applied at, and a resolved decision carries that
same list forward, so a point that has no standing over a capability (e.g.
one only enforceable at the server) does not act on it just because the
Decider returned "allow".

That one Decider is shared by all three enforcement points below, but they
are not otherwise identical — each has a different guarantee, and one of
them (the hook) cannot be bypassed by a network outage without also being
able to give up on purpose:

| Enforcement point | What it covers |
|---|---|
| **Server** (`ServerEnforcer`) | The only point with complete coverage once a call site invokes it — nothing routes around it, and it cannot time out into an implicit allow. It is implemented and tested (`server/internal/capability/enforcer_server.go`), but as of this writing no request path calls it yet: it enforces nothing in production today. |
| **Spawn** (`SpawnEnforcer`) | Complete for every agent the dashboard's task pipeline spawns itself: each granted `TaskPermission` is resolved through the Decider and rendered into that process's `--allowedTools` list (`server/internal/pipeline/spawner.go`). It cannot ask — the file is written before the process starts — so an `ask` decision is simply omitted, and the agent falls back to its own permission prompt for that call. |
| **Hook** (`HookEnforcer`) | The only point that can reach a session you started by hand, because it rides Claude Code's own `PreToolUse` hook instead of a start-time handshake. **It fails open on a timeout, by design** — see below. |

### The hook point's three outcomes

A hook call that gets no explicit decision looks the same from the terminal
in every case, but `HookEnforcer.Point()` (`server/internal/api/hooks/permission.go`)
distinguishes three situations that must not be flattened into one:

- **Actively vetoed** — the call matches one of your own `permissions.deny`
  rules. It is held and offered in the dashboard without an Allow button,
  and the server refuses to turn a "deny" into an "allow" even if the client
  is asked to. This is the one guarantee this enforcement point makes.
- **Never observed** — the session was never armed, or the hook payload was
  malformed. Nothing was evaluated at all, so this is neither open nor
  closed: it is exactly as if the hook were not installed, and Claude
  Code's own terminal prompt runs unmodified.
- **Deliberately lapsed — fails open.** A call was genuinely held (an armed
  session, a valid payload, no deny rule matched) and nobody answered within
  25 seconds. The hold gives up before Claude Code's own hook timeout does,
  on purpose, so a dashboard that is down, slow, or simply not being watched
  degrades the session back to its normal terminal prompt instead of hanging
  it forever. **State this plainly: an armed session nobody is watching is
  not protected any more strongly than an unarmed one.** The deny-rule check
  above is the only thing this enforcement point guarantees regardless of
  whether a human ever answers.

The hook point also does not consult the Decider's own pattern matcher for
its one active protection. The deny-rule check runs its own matcher
(`server/internal/claudesettings/deny.go:52-53`), which treats a
`domain:host` rule as matching whenever `strings.Contains(arg, host)` —
broader than the Decider's matcher (`server/internal/capability/pattern.go:84-97`),
which compares hostnames label by label so `example.com` cannot match
`evilexample.com`. This is deliberate, not an oversight: on the deny side,
matching *more* is the safe direction, because this matcher never grants
anything — it only decides whether to offer the Allow button, and the user
can still answer for real in their terminal. Swapping the Decider's strict
matcher onto the deny side would make deny rules match *less*, and start
offering Allow on calls the user's own settings already forbid. The two
matchers stay separate on purpose.

## Reporting a vulnerability

Please report security issues privately via [GitHub Security Advisories](https://github.com/lx-wnk/Agent-Dashboard/security/advisories/new) rather than opening a public issue.
