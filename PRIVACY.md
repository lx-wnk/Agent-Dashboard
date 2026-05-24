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

### Hooks secret (`~/.claude/dashboard-hooks-secret`)

Auto-generated on first start. File permissions are set to `0600` (owner read/write only). Rotate by deleting the file and restarting.

### VAPID private key

The VAPID private key used for Web-Push is stored in the SQLite database under a configuration key. It never leaves your machine unless you explicitly copy it. Treat database access as equivalent to key access.

---

## 3. Data That Leaves the Machine (Opt-In Only)

None of the following integrations are active by default. Data only leaves your machine when you explicitly configure and enable them.

### GitHub OAuth (`DASHBOARD_GITHUB_CLIENT_ID` + `DASHBOARD_GITHUB_CLIENT_SECRET`)

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

### OpenAI LLM adapter (if configured)

- If you configure an OpenAI-backed spawner, stage agent prompts (including task descriptions and stage outputs) are sent to `api.openai.com` (US).
- **Transfer basis:** OpenAI Inc. is a US entity covered by the EU–US Data Privacy Framework (DPF).

### Office365 OAuth plugin (if configured)

- If you configure the Office365 OAuth plugin, authentication flows through Microsoft Graph. Data residency depends on your Microsoft tenant region; DPF applies for US-based tenants.

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
- Configure GitHub OAuth (`DASHBOARD_GITHUB_CLIENT_ID` / `DASHBOARD_GITHUB_CLIENT_SECRET`) to enforce per-user authentication. Without OAuth, any process with access to `127.0.0.1:13120` has full dashboard access.
- Ensure appropriate access controls on the SQLite database file.

### Multi-machine mode

When using `DASHBOARD_REMOTES` to aggregate agents across machines:

- Session data (transcripts, tokens, costs) from remote instances is fetched and displayed locally.
- Remote instances must only be reachable via a VPN or SSH tunnel — never via a public IP or by binding to `0.0.0.0`.
- Each remote instance operator is independently responsible for the data practices described in this document.

---

## Questions

This is an open-source tool (MIT license). If you have questions about data handling, open an issue in the repository or inspect the source code directly — the relevant files are `server/internal/parser/`, `server/internal/merger/`, `server/internal/api/`, and the plugin directories.
