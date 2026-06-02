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

## Reporting a vulnerability

Please report security issues privately via [GitHub Security Advisories](https://github.com/lx-wnk/Agent-Dashboard/security/advisories/new) rather than opening a public issue.
