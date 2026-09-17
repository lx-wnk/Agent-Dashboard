# Applications in the App — Design Spec

> Adding, configuring and governing an MCP application happens entirely in the dashboard: the dashboard's database holds the server, an optional switch exports it to Claude Code sessions, the server's own setup UI opens from the app, and grants grow from real use — dangerous tools denied from the start, everything else asked on first use.

**Parent:** `2026-08-27-agenticos-overview-design.md` (D-level rule "the database is truth, config directories are produced from it"; K4 materializer)
**Builds on:** `2026-09-16-mcp-applications-and-mail-design.md` (slice 1, merged as #449)
**Sibling:** `2026-09-17-autonomous-routines-design.md` (permission decisions, §2.3)
**Owner rule (2026-09-17):** everything through the app, nothing through the console; changes apply without a restart.

---

## 1. Status Quo

Every reference was re-read on `main` at `4d4bc3d6`.

### What works

- **Applications exist in the registry** with attachment, required secret names and a tool catalogue (`server/internal/db/ent/schema/mcp_application.go:24-37`); secrets are encrypted and write-only.
- **The dashboard already speaks MCP as a client** to read `tools/list` (`server/internal/mcpapps/catalogue.go`).
- **The dashboard already runs `claude mcp add --scope user` server-side** during onboarding (`server/internal/api/onboarding/handler.go:106`).
- **A safe config writer exists**: the materializer writes atomically and refuses symlinks below the config root (`server/internal/materializer/apply.go:40`).
- **File watching needs no new dependency**: `fsnotify` is in `server/go.mod:13`.
- **Tools without a grant already ask**: class `tool` defaults to ask (`server/internal/capability/decide.go:236`).

### What does not

| # | Gap | Evidence | Consequence |
| --- | --- | --- | --- |
| A1 | The server definition lives in `~/.claude.json`, not in the app | Runs and refresh re-read it through `ReadServers` (`catalogue.go:71`, `server/internal/mcpapps/resolve.go:39,48`) | A server can only be added from the console (`claude mcp add`); the app mirrors, it does not own |
| A2 | No UI to add, edit or remove a server | Settings → Applications edits attachment, secrets and refresh only | The owner's rule cannot be met |
| A3 | Accounts of a server are set up on the console | `imap-mcp-server` accounts come from its wizard `imap-setup` (`dist/setup.js:2485`) writing `~/.imap-mcp/accounts.json` (`dist/index.js:1586`) | Mail cannot be configured from the app |
| A4 | The grant preset needs an up-front human judgement | `ApplyPreset` refuses while `"confirmed": false` (`server/internal/mcpapps/preset.go:19,53`) | The owner cannot judge 40 tools in advance and wants to decide on real use |
| A5 | The server's setup wizard listens on every interface | `app.listen(this.port)` without a host (`imap-mcp-server@2.0.0`, `dist/setup.js:2454`) | While it runs, account setup — passwords included — is reachable from the local network |

---

## 2. What Changes

### 2.1 The database holds the server

- `mcp_application.entry` (JSON): transport (`stdio`), command, args, non-secret env. Secrets stay in `application_secret`.
- `mcp_application.export_to_claude` (bool, default false) and `exported_hash` (the entry as last written to Claude's config).
- Runs and refresh read `entry` from the database; `ReadServers` is no longer consulted for them. A change in the app applies to the next run without a restart.

**One-time import.** A marker migration copies every user-scope server from `CLAUDE_CONFIG_DIR/.claude.json` (reserved `dashboard-channel`/`dashboard-tasks` excluded) into `entry`, with `export_to_claude = true` so existing Claude Code sessions keep them, and `attach_all` unchanged.

### 2.2 Managing servers in the UI

Settings → Applications:

- **Add server:** name (slug), command, args, non-secret env. stdio only (refresh supports stdio only). Saving triggers a tool refresh; a matching preset applies its default denies (§2.4).
- **Edit, remove.** Remove deletes the server, its secrets and its grants; refused with 409 while a routine attaches it, naming the routines.
- **"Also available in Claude Code sessions":** the materializer writes or removes `mcpServers.<name>` in `CLAUDE_CONFIG_DIR/.claude.json` — never secrets — changing only that key, atomically, keeping file mode and every other key, refusing symlinks.

**Changes from outside.** A `fsnotify` watch on Claude's config:

- An unknown user-scope server → banner "Found `<name>` — import?". Nothing is attached automatically.
- An exported entry whose content no longer matches `exported_hash` → "Changed outside the app" with "Take the change" / "Write the app's version back". Never overwritten silently.

### 2.3 Server setup UI

- An application may declare `setup`: command, args with a `{port}` placeholder, and a readiness path. Known servers declare it in their preset file; custom servers can enter it in the UI.
- For `imap-mcp-server`: `npx -y -p imap-mcp-server@2.0.0 imap-setup --no-open --skip-claude --port {port}`, readiness `/api/health`.
- **Start:** a click picks a free port, starts the process with the application's `entry` (same `HOME` the server gets at run time), waits for readiness, and opens the UI in an in-app panel (iframe) with an "Open in new window" fallback.
- **Stop:** one setup process per application; stopped on "Done", on closing the panel, after 15 minutes idle, after 30 minutes at most, and on dashboard shutdown.
- **A5:** the panel shows "Setup is reachable on your local network until you click Done". A follow-up upstream issue asks the server to bind `127.0.0.1`.

**Secrets from the accounts.** After "Done" the app calls the server's `imap_list_accounts` through its MCP client and derives required secret names from the preset's `secretTemplates` — `IMAP_MCP_ACCOUNT_{ACCOUNT}_IMAP_PASSWORD` and `…_SMTP_PASSWORD` — normalising the account name like the server does (upper-case, every non-alphanumeric character to `_`). The panel then shows one password field per derived name (write-only). This tool call is an operator action in the UI, not an agent action, and does not pass through grants; documented as such. The output shape of `imap_list_accounts` is verified first; if it carries no usable names, the operator types account names instead.

### 2.4 Grants from use, dangerous tools denied by default

- A preset file keeps `match` (the package in the entry's command, e.g. `imap-mcp-server`), `denyGlobal`, `setup` and `secretTemplates`. `allowForRoutine` and `confirmed` are removed, together with `ErrPresetUnconfirmed`.
- The default denies apply automatically when a server is added or imported, or when a refresh first recognises a preset. "Re-apply default denies" replaces `POST /api/applications/{id}/presets/{preset}` and is idempotent.
- Everything else asks on first use with the decisions of the sibling spec (§2.3): once, always for this routine, always deny for this routine, deny once.
- A request for a default-denied tool shows no decision buttons: the needs-you card says "denied by default — change it under Settings → Grants" and the run resumes with the refusal.
- Settings → Applications lists every tool with its current state: denied by default, asks, allowed for routine X. Server hints (read-only, destructive) stay informational only.

---

## 3. Error Handling

| Situation | Behaviour |
| --- | --- |
| Import finds an unreadable or malformed `.claude.json` | Marker not set, error logged and shown on the Applications page; the next start retries |
| Export target is a symlink or unwritable | Refused, switch reverts, reason shown |
| Setup command fails to start or never becomes ready | Process killed after the readiness timeout, stderr tail shown in the panel |
| `imap_list_accounts` fails | Manual account-name entry offered; nothing written |
| Remove while attached | 409 naming the routines |
| Refresh recognises no preset | No default denies; every tool asks |

---

## 4. Testing

Every new guard gets a test that goes red when the guard is removed, demonstrated and restored.

- Import: first start copies servers into `entry` with `export_to_claude = true`; a second start copies nothing (red without the marker); reserved names are skipped.
- Resolution: a run uses the database `entry` even when `.claude.json` holds a different command.
- Export: only `mcpServers.<name>` changes, every other key byte-identical; no secret value in the file; a symlinked target is refused (red without the refusal); turning the switch off removes the key.
- Watch: an unknown server raises "found"; a changed exported entry raises "changed outside" and is not overwritten.
- Remove: 409 while attached, with routine names.
- Setup launcher (fake command): start, readiness, stop on Done and on idle (red without the idle stop), one process per application.
- Secret names: a fake `imap_list_accounts` result `iCloud Privat` yields `IMAP_MCP_ACCOUNT_ICLOUD_PRIVAT_IMAP_PASSWORD`.
- Default denies: applied on add and on first recognition; applying twice creates no duplicates (red without the existence check); a request for a denied tool offers no decision and resumes.
- UI: add/edit/remove server, export switch, found and changed banners, setup panel with warning and password step, per-tool state list. Every mounting test unmounts.

---

## 5. Out of Scope

- HTTP/SSE MCP servers in the add form (refresh supports stdio only).
- Sending mail (mail spec slice 3) and the new-mail trigger (slice 2).
- Settings snapshot reload and UI parity for the remaining CLI-only actions — separate sub-projects of the "app is the workplace" direction.
- Patching `imap-mcp-server` itself.

---

## 6. Delivery

1. **Servers in the database** — §2.1, §2.2.
2. **Setup UI, grants from use, default denies** — §2.3, §2.4; needs the sibling spec's routine decisions.
