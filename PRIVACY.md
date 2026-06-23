# Privacy Policy

**Claude Agent Overview** is a local developer tool that runs entirely on your machine and binds exclusively to `127.0.0.1`. This document describes what data the tool reads, what it persists to disk, what (if anything) leaves your machine, and what it deliberately does not collect.

---

## 1. Data the Tool Reads

The dashboard reads the following data from your local filesystem at runtime. Nothing is transmitted unless you opt in to an integration described in section 3.

| Source | What is read | Why |
|---|---|---|
| `~/.claude/projects/{encoded_path}/*.jsonl` | Full Claude Code session transcripts — prompts, responses, tool calls, token counts | Real-time agent monitoring |
| `~/.claude/usage-data/session-meta/*.json` | Session metadata — model, start time, token totals | Cost and status display |
| `ps`, `lsof` (macOS) / `/proc/<pid>/cwd` (Linux) | Running process list, working directories | Matching PIDs to session files |
| `~/.claude/dashboard-tasks.db` | Task pipeline state (see section 2) | Pipeline orchestration |

**Important:** JSONL session files may contain sensitive information — API keys typed into prompts, file contents, credentials pasted during a session, or other PII. The dashboard does not sanitize or redact these files. You are responsible for protecting them using standard host-OS file permissions (e.g. `chmod 700 ~/.claude/projects/`).

---

## 2. Data the Tool Persists

All persistent data stays on your machine. No data is synced to a cloud service automatically.

### SQLite database (`~/.claude/dashboard-tasks.db`, override via `DASHBOARD_DB_PATH`)

| Table | What is stored |
|---|---|
| `tasks`, `stage_runs` | Task definitions, stage outputs (LLM-generated text), audit entries, status history |
| `permission_requests` | Tool permission requests from spawned stage agents |
| `users` | GitHub user ID, login, and avatar URL (only when GitHub OAuth is configured) |
| `api_keys` | SHA-256 hashes of bearer tokens — never the plaintext token |
| `web_push_subscriptions` | Web-Push endpoint URLs and VAPID keys (see section 3) |
| `spawners`, `presets`, `remotes`, `refine` | User-configured pipeline settings |
| `cost_history` | Aggregated per-session token and cost data imported from JSONL files |

### Hooks secret

Set via the `DASHBOARD_HOOKS_SECRET` environment variable. The secret is **always required** — if you do not set it explicitly, one is auto-generated and persisted on first boot (see `config.Load`). `hooks.New` panics at startup if a secret cannot be supplied, so there is no unauthenticated hook path.

### VAPID private key

The VAPID private key used for Web-Push is stored in the SQLite database under a configuration key. It never leaves your machine unless you explicitly copy it. Treat database access as equivalent to key access.

### Retention & deletion

There is **no automatic expiry** for any persisted data — rows live until you delete them. Concrete deletion paths:

| Data | How to delete |
|---|---|
| `users` (account record) | `DELETE /api/me` — permanently removes the authenticated user's account (GDPR right-to-erasure; `server/internal/api/auth/handler.go`, `DeleteMe`). |
| `tasks` + their `stage_runs`, `permission_requests` | `DELETE /api/tasks/{id}` — deletes the task and cascades its stage history. |
| `web_push_subscriptions` | Unsubscribe in the browser, or delete the row. |
| `audit_events`, `agent_cost_trends` | **No API delete path.** Prune manually with a SQLite client (e.g. `DELETE FROM audit_events;`) or delete the entire database file at `DASHBOARD_DB_PATH` (default `~/.claude/dashboard-tasks.db`). |

To erase everything at once, stop the dashboard and delete the database file at `DASHBOARD_DB_PATH`. Filesystem-derived monitoring data (the JSONL session logs under `~/.claude/projects/`) is never written by the dashboard — delete those files directly if needed.

---

## 3. Data That Leaves the Machine (Opt-In Only)

None of the following integrations are active by default. Data only leaves your machine when you explicitly configure and enable them.

### GitHub OAuth (`GITHUB_CLIENT_ID` + `GITHUB_CLIENT_SECRET`)

- OAuth flow redirects your browser to `github.com` (US). GitHub's privacy policy applies.
- Scope requested: `read:user` only.
- The access token is used once to fetch your GitHub user ID, login name, and avatar URL, then discarded. It is never stored.
- Only the GitHub user ID, login, and avatar URL are persisted locally (in the `users` table).
- **Transfer basis:** GitHub Inc. is a US entity covered by the EU–US Data Privacy Framework (DPF).

### Web-Push notifications (VAPID)

- If you subscribe to browser push notifications, your browser generates a push endpoint URL (hosted by Mozilla Autopush, Google FCM, or Apple APNs depending on your browser) and sends it to the dashboard.
- This endpoint URL is stored locally in the SQLite database and sent to the push service when a notification is dispatched.
- Push endpoint URLs are device-specific and qualify as pseudonymous personal data under GDPR.
- **Retention:** Push subscriptions persist until you unsubscribe in the browser or delete the database row. There is no automatic expiry — delete entries manually if no longer needed.
- **Transfer basis:** Mozilla (IE/EU entity), Google LLC (US, DPF), Apple Inc. (US, DPF).

### LLM adapters (OpenAI / Ollama / Anthropic / custom)

**OpenAI** — If you configure an OpenAI-backed spawner, stage agent prompts (including task descriptions and stage outputs) are sent to `api.openai.com` (US). **Transfer basis:** OpenAI Inc. is a US entity covered by the EU–US Data Privacy Framework (DPF).

**Ollama** — Defaults to `http://localhost:11434`; in the default configuration no data leaves the machine. If you configure a remote `base_url`, full stage-agent prompts (task descriptions, stage outputs, possibly source-code excerpts) are sent to that host. You are responsible for that host's data-residency and applicable transfer basis.

**Anthropic** — If you configure an `anthropic` spawner (`adapter_type: "anthropic"`), stage-agent prompts (task descriptions, stage outputs, and possibly source-code excerpts) are sent to `api.anthropic.com` (US) via the `anthropic-spawner` binary, which reads the `ANTHROPIC_API_KEY` environment variable from the server process. **Transfer basis:** Anthropic PBC is a US entity covered by the EU–US Data Privacy Framework (DPF) where applicable.

### Office365 OAuth plugin (if configured)

- If you configure the Office365 OAuth plugin, the OAuth flow redirects your browser to Microsoft identity endpoints. Data residency depends on your Microsoft tenant region; DPF applies for US-based tenants.
- **Scopes requested:** `openid profile email User.Read` (always). `GroupMember.Read.All` is added when `OFFICE365_ALLOWED_GROUP_ID` is set to gate access to a specific Azure AD group.
- When `GroupMember.Read.All` is requested, the plugin calls `/me/memberOf` on Microsoft Graph and reads the signed-in user's Azure AD group memberships to check for the required group ID. Group IDs are used only to evaluate membership in memory and are **not persisted**.
- The user's `UserPrincipalName` (an email address) is persisted as `login` in the `users` table, alongside the Microsoft user ID and display name. The access token is used once and then discarded — it is never stored.
- **What is persisted:** Microsoft user ID, UserPrincipalName (email address as `login`), and display name — in the `users` table. No group IDs are stored.

### Webhook and email notifications (forward-looking)

The notification-config API accepts and persists `webhook_url` (with HMAC authentication) and an `email` channel. **No delivery code for these channels exists yet** — as of the current release, configuring them stores the URL or address locally but nothing is sent via them. When and if delivery is implemented, task notification payloads (title, status, and stage output, which may contain source-code excerpts or other content from the task) will be sent to the configured SMTP server or webhook endpoint. You will be responsible for that endpoint's data handling and applicable transfer basis. This document will be updated when delivery is shipped.

### Remote dashboard proxy (`DASHBOARD_REMOTES`)

- If you configure remote instances, the local dashboard acts as a proxy and aggregates session data from those instances over your network.
- No data flows to a third-party service — only between self-hosted instances you control.
- **Security requirement:** Remote instances must be reachable only via a VPN or SSH tunnel. Never bind a remote instance to `0.0.0.0` on an untrusted network. The local dashboard logs a security warning if a non-loopback bind address is detected.

---

## 4. What Is NOT Collected

The following is explicitly absent from the codebase and has been grep-verified during an audit (2026-05-24):

- No telemetry or usage analytics
- No phone-home pings or update-check requests
- No Sentry, Datadog, or error reporting services
- No third-party analytics pixels or tracking scripts
- No advertising identifiers
- No background sync to any cloud service

The dashboard makes no outbound network connections unless you configure an integration in section 3.

---

## 5. Multi-User and Multi-Machine Responsibilities

### Multi-user machines

If you run this dashboard on a shared machine (e.g. a shared CI runner or team server) and other users can access it, you are acting as a **data controller** under GDPR for any personal data processed on their behalf. You must:

- Inform those users about the data practices described in sections 1–3.
- Configure GitHub OAuth (`GITHUB_CLIENT_ID` / `GITHUB_CLIENT_SECRET`) to enforce per-user authentication. Without OAuth, any process with access to `127.0.0.1:13120` has full dashboard access.
- Ensure appropriate access controls on the SQLite database file.

### Multi-machine mode

When using `DASHBOARD_REMOTES` to aggregate agents across machines:

- Session data (transcripts, tokens, costs) from remote instances is fetched and displayed locally.
- Remote instances must only be reachable via a VPN or SSH tunnel — never via a public IP or by binding to `0.0.0.0`.
- Each remote instance operator is independently responsible for the data practices described in this document.

---

## Questions

This is an open-source tool (MIT license). If you have questions about data handling, open an issue in the repository or inspect the source code directly — the relevant files are `server/internal/parser/`, `server/internal/merger/`, `server/internal/api/`, and the plugin directories.
