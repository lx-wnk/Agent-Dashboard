# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

From the first tagged release onward, release notes are generated automatically
from [Conventional Commits](https://www.conventionalcommits.org/) by GoReleaser.

## [Unreleased]

Preparing the first public release.

### Added

- Guided first-run onboarding flow: opens automatically on first launch (until dismissed) and walks through detecting the Claude Code CLI (version or install command), one-click MCP registration to connect the dashboard to Claude (with a copy-the-command manual fallback), and discovering existing Claude sessions to make one controllable with one click via `--resume`. New endpoints: `GET /api/onboarding/status`, `POST /api/onboarding/register-mcp`, `PATCH /api/onboarding/status`; new `onboarding.completed` setting tracks whether the flow has been dismissed or finished.
- **Re-run first-run setup** button under **Settings → Appearance**: re-opens the onboarding flow on demand without affecting the persisted `onboarding.completed` setting, so you can revisit the CLI/MCP/session steps after the initial run.
- A macOS desktop app shell (`desktop/`, [wails](https://wails.io) v2, `//go:build darwin`): one binary that both runs the dashboard server and opens a native WKWebView window for it, no sidecar process. It starts the existing HTTP server in-process via the new `server/serverapp` package (extracted so the server can be run either out-of-process by the `serve` CLI or in-process by a host like the desktop shell), waits for `/api/system/health` before opening the window, and cancels the server context on quit for a graceful drain. An embedded bootstrap page redirects the webview to `http://127.0.0.1:13120`, so the page runs on that loopback-http origin and the server's existing same-origin mutation guard applies unchanged. Non-macOS builds get a stub `main` that points users at `agent-dashboard serve`. Smoke-buildable today via `cd desktop && CGO_ENABLED=1 go build -o agent-dashboard-desktop . && ./agent-dashboard-desktop`. Distributable packaging via `task desktop:dist`/`desktop:dmg` (wails CLI) produces an unsigned `bin/Agent Dashboard.app`/`.dmg`; signing and notarization are a documented, deferred operator step — see [`docs/desktop-distribution.md`](docs/desktop-distribution.md).
- Live **Terminal** tab in the agent detail modal for spawned/live-injectable sessions — a real `xterm.js` terminal streamed over a WebSocket, so you can watch and interact with the session's actual pty output instead of a parsed transcript. New endpoint: `GET /api/agents/{pid}/terminal` — an authenticated WebSocket proxy that bridges the browser to the session's pty broker.
- Answer a Claude **AskUserQuestion** prompt directly from the Terminal tab: an overlay detects the question from the live terminal screen and renders the options inline (radio for single-select, checkboxes for multi-select); **Send answer** drives the session's selector with real keystrokes over the same WebSocket — the option's number hotkey for single-select, and Space/Down/Enter to toggle and confirm for multi-select (Claude's selector takes keypresses, not free text). Because it drives the pty directly, it works over any transport (tmux or the built-in pty broker), not just tmux.
- Answer an **AskUserQuestion** from the main **needs-you band** too, not only the Terminal tab — questions are detected server-side from the session's rendered screen (the pty broker's scrollback, or `tmux capture-pane`), so they surface as an answerable card wherever the agent appears. Answering posts to `POST /api/agents/{pid}/answer-question`, which derives the keystrokes and delivers them over the session's injection path (pty broker or tmux). Any injectable session — dashboard-spawned or started with `agent-dashboard live` — is now fully controllable from the dashboard.
- End-user install paths: binary (one-liner via `install.sh`), Homebrew cask on macOS (`brew install lx-wnk/tap/agent-dashboard`), and Docker (`ghcr.io/lx-wnk/agent-dashboard`) — no Go or build tools required at runtime. See [`docs/guides/install.md`](docs/guides/install.md).
- One-command MCP connect: the API key dialog's **CLI command** block copies a `claude mcp add --scope user --transport http …` one-liner with the generated key substituted. Documented in [`docs/guides/mcp.md`](docs/guides/mcp.md#connect-the-dashboard-to-claude).
- GoReleaser config now publishes a Homebrew cask to `lx-wnk/homebrew-tap` and a multi-arch Docker image (`linux/amd64`, `linux/arm64`) to GHCR on `v*` tag pushes. See [`docs/RELEASING.md`](docs/RELEASING.md).
- Issue → Task seeding: paste a GitHub or Jira issue reference into the **Import from issue** field in the New-Task form and click **Fetch** to pre-fill the title, slug, and description (issue body plus a `Source: <url>` line). GitHub personal-access tokens and Jira API tokens are stored encrypted at rest (reusing the existing AES-GCM secretbox) and masked in the settings UI (**Settings → Tracker**); unchanged secrets are preserved on save. New endpoints (JWT group, same-origin checked): `POST /api/tracker/fetch {ref}` resolves a GitHub or Jira issue by reference (full URL, `owner/repo#n`, bare `#n` with a configured default repo, or a Jira `KEY-123` / browse URL) and returns a normalised Issue (title, body, URL, labels), with typed error mapping (400 bad ref / missing config, 401 auth rejected, 404 not found, 502 upstream); `GET` / `PUT /api/tracker/settings` manage the encrypted tracker credentials.
- Per-turn checkpoint / revert for pipeline-task worktrees. A gitignore-aware filesystem watcher (~2 s debounce) captures each agent turn as a hidden git ref (`refs/checkpoints/<task>/<seq>`) using a temporary index, so the agent's index and HEAD are never touched. The task modal's new **Checkpoints** tab lists snapshots (newest first) with a Revert button; reverting kills the live agent, saves the current state as a recoverable pre-revert checkpoint, restores the worktree to the chosen snapshot, and parks the task (`awaiting_user`) for manual resume. Endpoints: `GET /api/tasks/{id}/checkpoints` and `POST /api/tasks/{id}/checkpoints/{cpId}/revert`; live updates over the `checkpoint_added` SSE event. Retention prunes to 50 checkpoints per task; refs and rows are cleaned up when the worktree is removed.
- `wait_for_port` MCP coordination tool — agents can block until a TCP service is reachable (max 300 s, returns `reached`/`timedOut`).
- Per-project `setup_command` field — run once in the worktree after creation; failure logs and continues.
- Prompt templates with `{{placeholder}}` fill-in — picker in the full prompt input, CRUD at `/api/prompt-templates`.
- Natural-language schedule phrases that the built-in rule set can't parse now fall back to an LLM translator (`claude -p`), so expressions like "first weekday of every month at 7am" resolve to a cron instead of failing. The LLM's output is re-validated as a 5-field cron before use.
- Token pricing for `gpt-5`, `gpt-5-codex`, `gemini-2.5-pro`, and `gemini-2.5-flash` so Codex and Gemini agents report a real cost instead of "unknown".
- Eval drift alerts are pushed live to the dashboard over SSE (`eval_drift` event) the moment a drift is detected, instead of only appearing on the next 60-second poll.
- `GET /api/usage` — rolling-window token and cost aggregator (5h session-equivalent, 7d weekly-equivalent) derived from session JSONLs across all configured Claude config dirs; replaces the permanently dead `/api/quota` endpoint.
- `usage.budget.session` and `usage.budget.weekly` settings (token counts; 0 = unset) to optionally derive a % bar in the status bar.
- Status-bar USAGE segment: worst-case % bar when a budget is set, compact consumption text otherwise; popover shows both windows, per-account breakdown when multiple accounts exist.
- `POST /api/admin/restart` triggers a validated, graceful restart. The endpoint refuses with **409** if an active `auth_provider` plugin is currently unhealthy (restarting in that state would cause an auth lockout on the next boot). Default `DASHBOARD_RESTART_MODE=reexec` replaces the process image in place (no supervisor needed); set `DASHBOARD_RESTART_MODE=exit` and run under systemd (`Restart=always`), launchd (`KeepAlive`), or a wrapper loop (`while true; do ./bin/agent-dashboard serve; done`) for supervised setups. Activating an `auth_provider` plugin requires a restart to apply — auth is boot-wired.
- `dashboard plugins list` / `disable <id>` / `enable <id>` CLI commands operate directly on the SQLite database (no HTTP, no auth gate), so a broken `auth_provider` plugin that prevents boot can be disabled offline. The change applies on next server start; lifecycle hooks are skipped (it is a recovery tool, not the normal activate path).
- Disabling a **UI-extension plugin** now prompts a page reload so the browser fully unloads its ES module code (browser ES modules persist in the registry until the page is reloaded).
- **Per-plugin settings UI**: a schema-driven form (`Settings → Plugins`) lets you view and edit each plugin's settings inline. Fields are rendered per type (`string`, `url`, `int`, `bool`, `enum`); secret fields are masked and unchanged secrets are preserved on save (the masked sentinel is sent to the server, which keeps the stored value).
- Plugin author SDK (`plugin-sdk/`): a `plugin.json` JSON Schema (draft-07) for editor
  autocomplete and validation, TypeScript UI-addon types (`addon.d.ts`) mirroring the
  host slot contracts, and a quickstart README covering the backend contract, lifecycle
  hooks, UI addon authoring, and per-capability liveness.
- Consolidated plugin developer guide (`docs/plugin-guide.md`): full manifest v2
  reference (slots with priority/mode, settings with encrypted-secret masking,
  lifecycle hooks, permissions), capability liveness table, slot composition modes
  (sibling/override/extend + parent handle), per-plugin settings UI, enable/disable
  lifecycle endpoints, offline CLI hatch, and a "Build your first plugin" walkthrough.
- Plugin process groups with group-kill (no orphaned child processes), suppression of
  restarts for intentionally stopped plugins, and crash supervision that marks a plugin
  unhealthy (HTTP 503) instead of silently removing it from the dispatcher.
- Automatic **`.env` file loading**: a `.env` in the working directory is now read at
  startup for both `task dev` (via air) and `./bin/agent-dashboard serve`. It uses the
  same `DASHBOARD_*` keys as the environment, and an explicit shell `export` always
  takes precedence over a file value. Previously `.env` was only a template for manual
  `export`/`cp` and was ignored by the server process — so settings like
  `DASHBOARD_FORCE_WORKTREES` had no effect unless exported by hand.
- Task **dependency enforcement**: the orchestrator now actually gates on `add_dependency`/`POST /api/tasks/{id}/dependencies` relationships. A task is not started until every upstream dependency reaches its `required_stage` (lazy picker-gate — no extra state, picked up automatically once the upstream finishes). When an upstream is **cancelled**, the downstream's `on_cancel_action` decides the outcome (`cancel` cascades the cancellation down the chain, `start` lets it proceed, `on_hold` leaves it parked). Enriched tasks now report `isBlocked` (waiting on an unfinished upstream) and `isUnsatisfiable` (an upstream reached a terminal stage it can never satisfy), which the board and task cards already surface. The REST create default for `onCancelAction` is aligned to `on_hold` to match the MCP tool and schema.
- Coordination primitives: shared **scratchpads** and lease-based **locks** via a new `agent:coord` MCP scope (`write_scratchpad`/`read_scratchpad`/`list_scratchpad`/`acquire_lock`/`release_lock`), with a read-only Coordination tab in the task modal. Locks use lazy lease expiry (no background sweep).
- Opt-in **plan-review** pipeline stage (`planMode`, default off): after the concept is approved, a planning agent auto-generates the execution plan and self-reviews it in one pass; the vetted plan is surfaced for your approval (Approve / Reject + feedback) before the implementation stage edits any files. Configurable per-task and as a per-project default.
- Plugin **lifecycle foundation**: plugin state is now DB-backed (`plugin` table, states `discovered`/`inactive`/`active`) with a lifecycle API (`GET /api/plugins`, `POST /api/plugins/{id}/{install|activate|deactivate|uninstall}`) and per-plugin settings (`GET`/`PUT /api/plugins/{id}/settings`). Manifest v2 adds optional `slots`, `settings`, `lifecycle`, and `permissions` sections to `plugin.json` (v1 manifests still load). Secret settings are encrypted at rest with AES-256-GCM using a new `DASHBOARD_SECRET_KEY` master key (auto-generated to `~/.claude/dashboard-secret.key` if unset) and masked in the API. The legacy `plugins.enabled` setting is superseded by the `plugin` table (migration is automatic on first boot). Activation's serving effects (route mounting, UI slots) land with SP2/SP4.
- `anthropic` spawner adapter — run pipeline stage agents and refinement chat against the Anthropic Messages API via an out-of-process binary, keeping the SDK out of the server module. Requires `ANTHROPIC_API_KEY` in the server env and the `anthropic-spawner` binary on `PATH` (or `DASHBOARD_ANTHROPIC_SPAWNER_CMD`). Default model: `claude-opus-4-8`.
- Documented using the existing `openai` adapter with OpenAI-compatible multi-model gateways (OpenRouter, Together AI, Inflection, …) — set `base_url` + `api_key_env` + `default_model`, no new code needed.
- Collapsible agent groups — when a grouping (project/status/model) is active on the
  dashboard roster, click a group header to collapse or expand it. On first load only
  the highest-priority non-empty group is expanded (Active, else Waiting, else Idle,
  else Finished); the rest start collapsed. Collapsed state is remembered per grouping
  mode in localStorage and survives live re-renders.
- Opt-in monitoring of Codex CLI, Gemini CLI, Junie CLI, and pi.dev agents via a declarative provider registry (off by default; enable via the `providers.enabled` runtime setting or custom descriptors in `DASHBOARD_PROVIDER_DIR`). pi.dev's per-session JSONL (`~/.pi/agent/sessions/`) carries token usage, model, provider, and cost per turn.
- Ollama-backed local models are now reported at $0 cost instead of unknown.
- Agents now show an animated **Working** badge while actively generating — derived from conversation turn-state plus live tmux/pty output — distinct from the idle/waiting staleness states.
- Agents spawned from the dashboard now run as interactive **live** sessions (tmux when available, headless pty broker otherwise) so you can converse with them live, instead of a one-shot run. The spawned agent's chat opens automatically once it appears.
- Controllable (channel/MCP-connected) agents now remain on the dashboard as a
  dismissable **Finished** card after their process exits, so results no longer
  disappear into the Sessions list. Click ✕ on a finished card to dismiss it
  (removes its channel discovery file). Finished cards are tracked per server run
  and appear in the card grid view. (#192)
- Live subtasks on agent and task cards — token usage, duration, and latest output for each active subagent are parsed from JSONL in real time and surfaced directly on the roster cards; child-task summaries are projected onto enriched tasks, gated on pid-liveness (PR #203).
- Per-stage spawner and model configuration — choose which engine (e.g. Codex for
  `self_review`, Claude Code for `implementation`) and which model runs each of the
  three agent stages (`implementation`, `self_review`, `finalization`). Settings are
  scoped globally or per-project; the resolution chain is: task `spawner_id` →
  project `stageSpawner.<stage>` → project `default_spawner_id` → global
  `stageSpawner.<stage>` → claude-default (for spawner); spawner `ModelOverride` →
  task `metadata.model` → project `stageModel.<stage>` → global `stageModel.<stage>`
  → coded default (for model). New endpoints: `stageSpawners` field added to
  `GET`/`PUT /api/pipeline/config`; new `GET`/`PUT /api/projects/{id}/pipeline-config`
  for project-scoped overrides. Drag-and-drop task reordering within stage columns
  is now supported. (PR #206)
- Per-task autonomy levels (`manual`, `spec_gated`, `full`) over REST and MCP
  `create_task`/`update_task`. New tasks default to `spec_gated`, which auto-approves
  permission requests (blanket Bash, audit-logged as `permission_auto_approved`);
  `manual` keeps the human-gated behaviour. Pre-existing tasks without the field stay
  gated. A self-describing `availableActions` set and an `approve_all_pending` control
  let blocked tasks be unblocked in one step (PR #200).
- Natural-language cron scheduling for pipeline tasks — describe a cadence in plain
  English ("every weekday at 9am") and the dashboard stores it as a cron expression,
  firing offline and deterministically. New REST endpoints under `/api/schedules`
  and MCP tools `list_schedules` / `manage_schedule` (PR #197).
- Multi-provider session resolution — Codex and Gemini agents resolve their JSONL
  session logs under each provider's own config dir, so foreign CLI agents can surface
  in the roster (cost reported as unknown until a real foreign session schema lands)
  (PR #199).
- In-dashboard `~/.claude` config explorer — browse and edit skills, slash commands,
  and memory files from the UI without leaving the dashboard (PR #190).
- Git worktree panel — create and remove worktrees for pipeline tasks directly from
  the task UI (PR #193).
- Opt-in HTTP hook receiver (`/api/hooks/event`, `/api/hooks/pre-tool`) for
  per-event agent rescans, gated by an auto-generated shared secret — keeps the
  no-hooks default intact. The receiver records a bounded per-session history of
  recent lifecycle-hook events (`PreToolUse`, `PostToolUse`, `Stop`, …) and
  surfaces them in the agent modal under **Hook events**; payload previews are
  truncated to 512 bytes and nothing is written to disk. The cap is controlled
  by `DASHBOARD_HOOK_EVENTS_PER_SESSION` (default 50). Stale sessions are swept
  on every record call (PR #205).
- Frontend plugin slot framework — named extension points (`refinement`, `settings`,
  and others) that sidecar plugins can mount custom UI into (PR #168).
- Lean, front-door `README.md` and a structured `docs/` tree (configuration, MCP,
  agent control, security, statusline, architecture overview).
- Release tooling: GoReleaser config + `release` workflow producing cross-compiled
  binaries (macOS/Linux, amd64/arm64) with the SPA embedded, plus an `install.sh`
  one-liner installer.
- `agent-dashboard --version` (version injected at build time via ldflags).
- `task setup`, `task doctor`, `task build:frontend`, and `task build:all` to lower
  the contributor entry barrier.
- Community health files: `SECURITY.md`, `CODE_OF_CONDUCT.md`, Dependabot config,
  and `FUNDING.yml`.
- Settings → Providers panel to enable or disable Codex/Gemini/Junie monitoring per provider, persisted in the database and applied within one scan tick.
- **DB-backed settings store**: operational config (auth mode, rate limits, scan
  intervals, plugin/provider enablement, `git.allowPush`, `git.allowPull`,
  `spawn.allowedCommands`, `worktree.force`,
  eval/drift tuning, …) now lives in the database `app_setting` table — the single
  source of truth is `server/internal/settings/registry.go`. Edit it in the new
  generic **Server** settings panel (registry-driven) or via the new direct-DB
  `dashboard settings list|get|set` CLI. The CLI edits the SQLite file directly, so
  it works while the server is down and is the **lockout-safe recovery path** (e.g.
  `dashboard settings set auth.mode none`). DB resolution: `--db` → `DASHBOARD_DB_PATH`
  → `~/.claude/dashboard-tasks.db`.
- Live **plugin enable/disable** via the lifecycle API — discovered plugins now default
  to **all-off** and are activated individually via the backend lifecycle endpoints
  (`POST /api/plugins/{id}/activate|deactivate`), applied live without a server restart.
  The Plugins settings-panel toggle that drives these endpoints is wired in SP4b.
- **Server reconnect overlay** — a full-screen blocking overlay appears automatically
  whenever the server goes down (e.g. during a restart). The UI polls
  `/api/system/health` every 1.5 s and reloads the page on the first successful
  response, recovering without any user action. Plugins with the `auth_provider`
  capability are **boot-wired** (they affect server startup) and cannot apply live:
  after toggling an `auth_provider` plugin the settings panel shows a
  "Restart required to apply" badge and a **Restart server** button, which triggers
  the restart and hands off to the reconnect overlay automatically.
- Plugin slot composition -- multiple addons targeting the same slot can now declare a
  `priority` (number; higher renders outer/first) and a `mode` (`override` to own the
  slot exclusively, `extend` to wrap the lower-priority chain). An `extend` addon's
  `mount(el, ctx, parent)` receives a `parent` handle it may invoke to compose the addons
  below it. Addons with no mode remain independent siblings and are unaffected.

### Changed

- Interactive question answering moved from the JSONL-derived "Needs answer" card in the **Needs you** triage band to the Terminal tab's live overlay (see Added, above). The old flow only worked for tmux-backed sessions and left non-tmux sessions read-only; the new one works for any live-injectable session, driven over the terminal WebSocket instead of parsed JSONL.

### Removed

- The triage-band "Needs answer" question card and its `POST /api/agents/{pid}/answer-question` endpoint — superseded by the Terminal tab's question overlay.

### Fixed

- `formatCost`/`formatTokens` no longer throw on `undefined`/`null`/`NaN` input — a partial `Agent` (missing cost or token fields) silently aborted the agent detail modal's rendering; both now degrade to the existing "no data" (`—`) display.
- The AskUserQuestion terminal overlay again appears on current Claude Code releases: detection matched the meta-row copy exactly, but v2.1.205 renders `Type something.` with a trailing period, which silently disabled the overlay. Detection now normalizes the meta-row copy (trailing punctuation, prefix) so cosmetic wording tweaks no longer break it.
- The AskUserQuestion terminal overlay now traps keyboard focus: Tab and Shift+Tab cycle within the modal instead of leaking back to the inert terminal underneath, matching the ARIA modal-dialog pattern.
- Non-Claude provider agents (Codex, Gemini, Junie, pi) no longer always show as `idle`: the provider parser now reads each session's activity timestamp, so their status reflects real activity.
- Plugin UI slots now correctly load manifests and modules from the SP2 proxy path
  (`/api/plugins/{id}/proxy/ui-manifest.json`, `/api/plugins/{id}/proxy/{module}`).
  The loader was still pointing at the pre-SP2 settings path after the route migration,
  causing all `ui_extension` addons to silently fail to load.

### Changed

- `ProjectRepo.Create` and `Update` accept `setup_command` (nullable).
- `worktree.force` setting now defaults to `true` — pipeline tasks automatically create a git worktree per task without requiring explicit `SourceBranch`. Set to `false` to restore the previous opt-in behaviour.
- Plugin route extensions now serve under `/api/plugins/{id}/proxy/*` and enable/disable
  live via the lifecycle endpoints (`POST /api/plugins/{id}/activate|deactivate` — no
  server restart). The interim `PATCH /api/settings/plugins-enabled/{id}` and
  per-plugin boot-mounted routes are removed.
- Agent card redesign — prominent, readable project name; a compact
  cost · tokens · uptime metric row with the full labeled detail (last activity,
  burn rate, cache costs) moved into a hover ⓘ popover and the agent modal; the
  prompt input is now always docked at the bottom with a larger output area.
- Accessibility: clickable agent rows are now native `<button>` elements, and the
  agent-modal summary uses a higher-contrast token.
- SSE poll and retry intervals are centralized in `src/utils/sse.ts` instead of
  being hard-coded at call sites.
- **BREAKING — env vars moved to DB-backed settings.** These `DASHBOARD_*` variables
  are **no longer read** from the environment; configure them in the Settings UI or
  with `dashboard settings set <key> <value>` (a still-set env var is ignored and
  logs a warning on boot): `DASHBOARD_AUTH` → `auth.mode`,
  `DASHBOARD_PROVIDERS_ENABLED` → `providers.enabled`,
  `DASHBOARD_ALLOW_GIT_PUSH` → `git.allowPush`,
  `DASHBOARD_ALLOW_GIT_PULL` → `git.allowPull`,
  `DASHBOARD_FORCE_WORKTREES` → `worktree.force`,
  `DASHBOARD_SSE_INTERVAL_MS` → `sse.intervalMs`,
  `DASHBOARD_SHUTDOWN_TIMEOUT_SECONDS` → `shutdown.timeoutSeconds`,
  `DASHBOARD_HOOKS_DEBOUNCE_MS` → `hooks.debounceMs`,
  `DASHBOARD_HOOK_EVENTS_PER_SESSION` → `hooks.eventsPerSession`,
  `DASHBOARD_SPAWN_RATE_LIMIT` / `DASHBOARD_SPAWN_RATE_WINDOW_MS` →
  `spawn.rateLimit` / `spawn.rateWindowMs`,
  `DASHBOARD_SPAWNER_ALLOWED_COMMANDS` → `spawn.allowedCommands`,
  `DASHBOARD_INJECT_RATE_LIMIT` / `DASHBOARD_INJECT_RATE_WINDOW_MS` →
  `inject.rateLimit` / `inject.rateWindowMs`,
  `DASHBOARD_COST_SCAN_INTERVAL_MS` → `cost.scanIntervalMs`, and the
  `DASHBOARD_EVAL_*` family → `eval.scanIntervalMs` / `eval.windowHours` /
  `eval.minSamples` / `eval.rateDropPP` / `eval.stddevK`.
- **BREAKING — plugins now default to all-off.** Previously every plugin found in the
  plugin directory loaded automatically; you must now enable each plugin explicitly
  (Plugins settings panel or `plugins.enabled`).
- Apply semantics: plugin and provider enablement apply **live**; all other settings —
  **including `auth.mode`** — require a **server restart** to take effect.

### Removed

- `/api/quota` handler and `usage-data/*.json` file reader (Claude Code never writes that file; the endpoint always returned null).
- Pruned unused TS-era dependencies never imported by the shipped app: `express`,
  `nodemailer`, `web-push`, `cookie-parser`, `supertest`, and their `@types/*`.
- Inert `permissions` and `plugin.json slots[]` manifest fields removed from the `Descriptor` Go type and `plugin.schema.json`. Both were parsed but never enforced or consumed — a security-shaped field with no enforcement is misleading. Slot bindings for UI extensions are declared in `ui-manifest.json` (authoritative since SP4a); the `plugin.json` copy was a divergeable duplicate. Old manifests carrying these fields still load correctly (`additionalProperties: true`). The unused `Registry.AllWithCapability` method is also removed.

### Fixed

- Plugin enable/disable in the UI now calls the live lifecycle endpoints (`POST /api/plugins/{id}/activate` and `/deactivate`) instead of the `PATCH /api/settings/plugins-enabled/{id}` endpoint that was removed in SP2. Plugins with the `auth_provider` capability still require a server restart after activation; the UI surfaces this with a notice.
- Plan-review gate: opening a `plan_review` task via deep-link, Spotlight, or
  cross-modal navigation now routes to the plan-approval panel instead of the
  generic task modal — the routing rule that previously lived only in the
  pipeline-board click handler is now applied to every navigation path. (#221)
- Plan-review panel now refreshes while a plan is still generating instead of
  showing "No plan output available yet" indefinitely: it polls the plan status
  until the gate settles, and re-fetches when reopened for a different task. (#222)
- Agent cards no longer show "No output yet" while an agent is actively working —
  the card now falls back to the current action / last tool when there is no
  assistant text yet.
- Startup: databases created before the per-stage pipeline config landed stored
  `pipeline_configs` as a bare (key, value) table; the new `(project_id, key)` index
  forced a table rebuild that failed with `NOT NULL constraint failed: ...id`. The DB
  layer now rebuilds the legacy table into the scoped `(id, key, project_id, value)`
  shape before auto-migrate (id backfilled from key, existing settings preserved),
  so existing installs upgrade cleanly. Idempotent (PR follows #206).
- Pipeline: a task reaching a terminal stage (`done`/`cancelled`) left its git
  worktree on disk, keeping its `source_branch` checked out. Because the
  duplicate-branch guard only inspected non-terminal tasks, a new task assigned the
  same branch then failed worktree creation. Terminal tasks now auto-remove their
  worktree (force, freeing the branch and reclaiming disk; cancellation discards
  uncommitted work, audit-logged as `worktree_removed`). The branch-collision
  preflight is now authoritative against git (`git worktree list`), so a branch held
  by any leftover or manual worktree yields a descriptive error on task create/refine
  and at worktree creation.
- Performance: the cost heatmap resolved chart color tokens once per cell
  (10 DOM probes × 168 cells per render); it now resolves them once per render and
  splices the per-cell alpha, which also stops the component test from timing out
  on slower CI runners.
- Pipeline: a task whose git worktree could not be created (e.g. its `source_branch`
  was already checked out by another worktree) silently stalled forever in
  `implementation` — the orchestrator returned a bare error before any stage run was
  created, so the picker just retried every tick with nothing surfaced in the UI.
  Worktree-creation failures are now recorded as a failed stage run and follow the
  normal retry/requeue path.
- Pipeline: creating or refining a task now rejects a `source_branch` already used by
  another active (non-terminal) task, preventing the duplicate-branch state that caused
  the silent stall (one branch can back at most one worktree).
- Accessibility: the light-mode `--fg-faint` text token now meets WCAG 2.2 AA
  contrast (4.97:1) on raised surfaces; it previously fell to 4.34:1 on
  `--raised`, below the 4.5:1 threshold at the small sizes used across the UI.
- Accessibility: the login gate now uses a `<main>` landmark and an `<h1>`
  heading, moves keyboard focus to the login control when it appears, and
  announces OAuth failures (`?error=`) via a `role="alert"` region.
- Worktree panel now emits a `change` event after create/remove so the task view
  can react; previously the action ran but the parent was never notified.
- Production build now embeds the real Vue SPA. `vite build` writes to
  `server/frontend/dist` (the `go:embed` source); previously it emitted to the
  repo-root `./dist`, so `task build` silently shipped the placeholder frontend.

### Security

- Resolve all `pnpm audit` advisories: bump `dompurify` to `>=3.4.11` (the only
  production-reachable one) and `vite` to `>=6.4.3`; pin transitive `undici`
  (`^7.28.0`), `esbuild`, `@babel/core`, and `brace-expansion` via workspace
  overrides. `pnpm audit` reports no known vulnerabilities.
- Hardened the live-injection endpoint (`POST /api/agents/{pid}/message`): rate
  limiting, audit logging, per-session token rotation, and control-character
  sanitization (PR #188).

[Unreleased]: https://github.com/lx-wnk/Agent-Dashboard/commits/main
