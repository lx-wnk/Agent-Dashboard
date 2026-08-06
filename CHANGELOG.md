# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

From the first tagged release onward, release notes are generated automatically
from [Conventional Commits](https://www.conventionalcommits.org/) by GoReleaser.

## [Unreleased]

Preparing the first public release.

### Added

- Maskable PWA icon: the web-app manifest now declares a `purpose: 'maskable'` 512×512 icon, so installed-PWA launchers on Android/Chrome render the icon in their adaptive shape without letterboxing. Also added a `docs/launch-checklist.md` operator playbook for the public launch (release tag, README hero, `good-first-issue` labels, social preview, awesome-list submissions).
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
- CI job matrices all derive from one place. The module list was hard-coded in six spots across `ci.yml`; a `matrix` job now holds `WORKSPACE_MODULES` and `PLUGINS` once and every other job's matrix reads its outputs, with the security list computed from the other two so it cannot drift. Adding a plugin means one entry in `PLUGINS`. (The lists live in a job output rather than a workflow-level `env:` because GitHub does not expose the `env` context to `strategy.matrix`.) Alongside it, the Go toolchain is pinned once in a new **`.go-version`** file that every `setup-go` step reads, and the `go` directives in the nine module files drop back to a plain `go 1.26` — a minimum requirement, as intended, instead of the de-facto build pin they had become. A toolchain bump is now a one-line change instead of nine, and a **Toolchain consistency** step fails the build if any module drifts from `.go-version` or grows a `toolchain` directive (Go has no mechanism to inherit the `go` directive, so agreement is checked rather than derived). `dependabot.yml` uses the globbing `directories` key for the same reason.
- Go dependencies moved to their current minor/patch releases: `chi` 5.3.1, `go-sdk` 1.7.0, `fsnotify` 1.10.1, `sqlite` 1.55.0, `libc` 1.74.1, and `x/sync`/`x/sys`/`x/term`. Two consequences worth knowing: chi 5.3.1 registers the HTTP `QUERY` method on wildcard mounts, which adds `QUERY /*` and `QUERY /api/plugins/{id}/proxy/*` to the route set — both are covered by the existing guards, because `RequireSameOriginForMutations` allowlists `GET`/`HEAD`/`OPTIONS` and treats every other method as a mutation, and the plugin proxy is mounted inside the authenticated group. And go-sdk 1.7.0 deprecates MCP logging (SEP-2577), which is how the dashboard delivers messages to a connected agent; the deprecation removes the feature rather than replacing it, so the call is kept and the warning suppressed at the call site until a different transport is designed. The SDK keeps it working for at least twelve months.
- Every dropdown in the app is now a custom listbox instead of a native `<select>`. WKWebView renders native select popups as macOS system chrome — system font, light background, no dark mode — and CSS cannot reach the option list, so the desktop app showed foreign-looking dropdowns throughout. The replacement builds the option list from real DOM (ARIA select-only combobox: `role="combobox"` trigger, teleported `role="listbox"` panel, full keyboard support including type-ahead), so it is themed like the rest of the app in both the browser and the desktop shell. The migration also removed the styling drift the 31 native elements had accumulated: hardcoded `blue-500` focus borders, `focus:` rings that fired on click rather than only on keyboard, ring widths that disagreed with the app's 3px, and one plugin enum field that carried no styling at all — replacing the four ad-hoc vertical paddings with two deliberate size variants, `default` and `compact`, so a select renders at the same height as whichever sibling control it's paired with (WorkflowsView's filter bar, TemplatePicker). The compact selects in TaskDependenciesTab's dependency row are an intentional exception: they match the row's other 11px controls, not the taller `AppInput` beside them.
- Desktop-shell build tasks renamed to the `build:`/`dev:` prefix used by every other task: `desktop:build` → **`build:desktop`**, plus a new **`dev:desktop`** for wails hot-reload. `build:all` stays server-only — the Playwright suite builds through it and must not start needing Xcode CLT — and the new **`build:everything`** adds the desktop shell on top. `build:frontend` is now `run: once`, so a chained build cannot compile the SPA twice.

- Interactive question answering no longer reads the JSONL transcript: both the **Needs you** triage band card and the Terminal tab's overlay are driven by the session's live rendered screen (see Added, above). The old flow only worked for tmux-backed sessions and left non-tmux sessions read-only; the new one works for any live-injectable session.

### Removed


### Fixed
- The slash-command menu now shows each command's **argument template** next to its name — `/branch-review` displays `[base-branch] [--apply-fixes]`, `/review-and-fix` its full flag list. The templates were always there in the command files' `argument-hint:` frontmatter, but the dashboard's frontmatter parser only ever read `description:`, so the field was dropped before it reached the API and the menu had nothing to render. Options were therefore only discoverable by reading the description prose, which for skills is the entire trigger paragraph. The parser now also reads `argument-hint:` (in either key order, and correctly unquoting YAML's doubled-apostrophe and backslash escapes, which real hints use), `SlashCommand`/`CommandDetail`/`SkillEntry` carry it, `GET /api/slash-commands` returns it as `argumentHint`, and the menu renders it. Skills are included: each is typeable as `/<name>`, so the hint survives the skill → command conversion. Built-ins show no template — they have no file on disk to read one from.
- `scripts/gen-licenses.sh` can no longer silently drop an entire Go module from `THIRD_PARTY_LICENSES.md`. On PR #329, Dependabot bumped a module without running `go mod tidy`; `go-licenses` died outright on it, and the script's `|| true` tolerance — there to swallow benign classification failures, not build failures — swallowed that too, so the Go dependency count silently fell from 72 to 63 while the script still exited 0. The script now (1) runs `go mod tidy -diff` for every scanned module before collecting anything and fails immediately, naming the module directory and the exact `go mod tidy` command to fix it, and (2) fails per-module if a module whose `go.mod` declares external requires ends up contributing zero rows, naming that module directory too. `MIN_GO_DEP_ROWS` remains as a total-collapse floor; these two guards close the gap where a single module can vanish without tripping it.
- The orphan-run sweep now also reaps a `running` stage_run left behind by a crashed or killed orchestrator process — previously only non-terminal runs on a parked task, `on_hold` runs with a dead PID, and stuck `pending` runs were reaped, so a server killed mid-dispatch (before a PID was ever persisted) left a `running` row that nothing would ever pick back up. The new check only fires when the run started *before* this orchestrator process did, so a legitimately long-running async HTTP/LLM-adapter stage (which spawns no local process at all) is never mistaken for an orphan. What happens to such a run is decided by the same recovery rule the server applies at startup: a live PID is left alone, a run with a recorded session is re-queued and respawned with `--resume`, and only a run with nothing to resume from is failed.
- Token totals and costs no longer leak between unrelated sessions on Linux. The parser's incremental token-scan cache was keyed solely by inode number, and Linux filesystems (ext4, overlayfs) recycle a freed inode number almost immediately — so a newly created session file that inherited a deleted session's inode resumed that session's cached byte offset and running total, reporting a lifetime token count and cost that belonged to a session that no longer exists. macOS/APFS effectively never recycles inode numbers, which is why this only ever showed up on Linux. The cache is now keyed by path and each entry is pinned to the inode it was seeded from, so a recycled inode at a different path can never be resumed while same-path log rotation is still detected.
- Spawning an agent from inside the macOS desktop app no longer opens a second desktop window instead of starting the agent. The spawner re-executes the running binary as `<self> pty-host -- claude …` and points the MCP config at `<self> channel`; inside the desktop app `os.Executable()` is the wails binary, which parsed no arguments at all and went straight to opening a window, leaving the spawn request hanging on a PID line that never arrived. Both subcommand names now live in one place and are dispatched by a shared, cobra-free entry point that the CLI and the desktop shell both call before anything else starts.
- The macOS desktop app no longer hangs when you quit it. `http.Server.Shutdown` waits for active connections but never cancels their request contexts, and every SSE handler only observes `r.Context().Done()` — so with the dashboard open its permanent streams kept the drain running into the full `shutdown.timeoutSeconds` (10 s) before it gave up with `context deadline exceeded`, and the desktop shell blocks on that drain while closing. Request contexts now derive from a context that outlives the start of shutdown by a short grace window (`RequestGraceDefault`, 2 s) before being cancelled, so an ordinary in-flight request (a DB write, a git commit, a worktree operation) still gets to finish instead of being torn out mid-mutation, while the permanent streams still release `Shutdown` well inside `shutdown.timeoutSeconds` once that grace window elapses (measured: 10.0 s down to ~0.27 s). The shell's drain wait is additionally bounded, and set above the server's own `shutdown.timeoutSeconds`, so a future handler that ignores its context cannot make the app unquittable.
- Agent cards keep their prompt input out of the way until you need it: it is revealed on hover or keyboard focus instead of occupying a row on every card. The slash-command menu opens downward in that compact layout, since the reveal wrapper clips anything above the input.
- The `waiting` agent status now reads **Quiet** everywhere (card, roster row, detail modal) instead of "Waiting", which collided with the needs-you band's "waiting for you". It means 30 s–5 min without new activity — the process is alive and nothing is blocked on you; the card's badge tooltip spells that out.
- Status and resource badges carry explanatory tooltips, the card output preview fades out instead of cutting mid-line, and the CPU/MEM/DISK readouts turn amber/red on the same thresholds as the usage bar.

- An AskUserQuestion answer picked in the needs-you band no longer gets cleared while you are still choosing. Two independent causes: the option rows of every question card shared one hard-coded radio `name`, and a radio `name` is a document-wide group — so with two cards on screen (two agents with a question, or a band card plus the terminal overlay) the browser silently unchecked the first card's radio, and Vue never repaired it because its bound `:checked` value had not changed. Each card now gets its own group name. Second cause: the question card reset its local state whenever the `pendingQuestion` prop object changed identity — and every SSE frame (every ~3 s, driven by volatile fields such as uptime) delivers a freshly deserialized object, so selections and typed custom answers were wiped mid-answer unless you clicked fast. The card now keys its reset off the screen's *content* signature, so only a genuinely different question clears what you entered.
- The AskUserQuestion **review/submit screen** ("Ready to submit your answers?" → Submit answers / Cancel) is now detected and answerable, in the needs-you band and the Terminal overlay. It carries none of the meta-rows the question detector requires, so a multi-question flow went invisible to the dashboard exactly at the final keypress and could not be completed from the UI at all. Detected server-side alongside the modal via the pty broker's new `GET /screen` endpoint (the older `GET /question` is still honoured as a fallback for broker processes started before the upgrade) or a single `tmux capture-pane` snapshot, and delivered over the existing `POST /api/agents/{pid}/answer-question`. Agents carry it on a new `pendingConfirm` field.
- The needs-you band no longer offers **Allow AskUserQuestion**. An open question is an unresolved `tool_use`, so it surfaced through the same pending-tool path as a real permission request — but it waits for an *answer*, not a grant, and the button would have written a standing allow-rule for a tool nobody has to approve. The underlying signal is kept (it is the only thing a session whose screen cannot be probed can show) and now reads "Waiting for your answer — open the terminal to reply".
- Task export (CSV/JSON) in the pipeline board now downloads via a hidden `<a download>` link instead of `window.open`, which silently opened no window (and thus downloaded nothing) in the desktop shell's WKWebView. The worktree panel's copy-path button and the API-key/onboarding copy-to-clipboard buttons now route through the shared `useClipboard({ legacy: true })` fallback instead of a bare `navigator.clipboard.writeText`, so they degrade to `execCommand('copy')` instead of failing silently under WKWebView's stricter clipboard permissions.
- `formatCost`/`formatTokens` no longer throw on `undefined`/`null`/`NaN` input — a partial `Agent` (missing cost or token fields) silently aborted the agent detail modal's rendering; both now degrade to the existing "no data" (`—`) display.
- The AskUserQuestion terminal overlay again appears on current Claude Code releases: detection matched the meta-row copy exactly, but v2.1.205 renders `Type something.` with a trailing period, which silently disabled the overlay. Detection now normalizes the meta-row copy (trailing punctuation, prefix) so cosmetic wording tweaks no longer break it.
- The AskUserQuestion terminal overlay now traps keyboard focus: Tab and Shift+Tab cycle within the modal instead of leaking back to the inert terminal underneath, matching the ARIA modal-dialog pattern.
- Non-Claude provider agents (Codex, Gemini, Junie, pi) no longer always show as `idle`: the provider parser now reads each session's activity timestamp, so their status reflects real activity.
- Plugin UI slots now correctly load manifests and modules from the SP2 proxy path
  (`/api/plugins/{id}/proxy/ui-manifest.json`, `/api/plugins/{id}/proxy/{module}`).
  The loader was still pointing at the pre-SP2 settings path after the route migration,
  causing all `ui_extension` addons to silently fail to load.
- Auto-approved permission requests now store the canonical `granted` outcome instead of `approved`, which broke the client's outcome-based rendering for auto-approved requests.
- A task run that hit its iteration limit no longer writes `done` and then immediately overwrites it with `failed` — the final stage run now reflects the actual outcome on the first write.
- The refinement chat now surfaces file-read errors instead of hanging indefinitely when a referenced file cannot be read.
- Removed dead computed properties and an unused watcher left over from earlier refactors in `App.vue` and `ProjectSettings`.
- The GitHub OAuth plugin now returns a clear "authentication denied" response when a user declines consent, instead of a misleading "missing code" error.
- The status line no longer mislabels cumulative session cost as an hourly rate (shows `$x.xx` instead of `$x.xx/h`).
- The Anthropic spawner plugin now fails a stream on accumulation errors instead of continuing with incomplete state that could defeat its refusal / max-token detection.
- History import now shares one implementation across the API-key settings and cost-analytics views, so the API-key view gains the "already running" (409) and malformed-frame handling it previously lacked.
- The parser session-cache TTL now tracks a configured non-default SSE interval, so idle-agent caching is not silently defeated when the scan loop is slowed down.
- `Registry.Shutdown()` now actually waits for plugin processes to exit before returning, and SIGKILLs any straggler that ignores SIGTERM instead of leaving the escalation in a goroutine that died with the process. Previously the wait-then-escalate step ran detached, so `Shutdown` returned immediately — the server process could exit right after, and a plugin that ignored SIGTERM was never killed. All plugins are signalled and waited on against one shared 5s deadline (not 5s per plugin), so stopping several unresponsive plugins costs one timeout, not a multiple of it.

### Accessibility

- The xterm terminal view now enables screen-reader mode.
- The Task modal's close button now has an accessible label.
- Toast auto-dismiss now pauses while a toast has keyboard focus.

### Docs

- `PRIVACY.md` now discloses the issue-tracker import feature (GitHub / Jira) as an opt-in outbound data transfer.
- Fixed a duplicate `ADR-0006` filename collision in `docs/architecture/adr/` — the eval-drift-detection ADR is renumbered to `ADR-0008`.

### Security

- `plugins/oauthkit` is now vulnerability-scanned. It was present in the test and lint matrices but absent from the security matrix, so `govulncheck` had never run against the module that holds the shared OAuth CSRF and session handling. Deriving all matrices from one source closes the gap and prevents the next one.
- Dependabot now covers every Go module. It was configured for four (`server`, `sdk`, `github-oauth`, `office365-oauth`) out of eight, leaving `desktop`, `oauthkit`, `voice-whisper`, `voice-webspeech` and `anthropic-spawner` without dependency updates.
- Go toolchain raised to 1.26.5 (pinned in `.go-version`), clearing [GO-2026-5856](https://pkg.go.dev/vuln/GO-2026-5856) / CVE-2026-42505 in the standard library's `crypto/tls`: TLS handshakes using Encrypted Client Hello could be de-anonymized by a passive network observer, because pre-shared-key identities were disclosed in the unencrypted client hello. `govulncheck` evaluates the standard library of the toolchain actually in use, not the `go` directive, so the pin is what closes this.
- `golang.org/x/text` raised to v0.39.0, clearing [GO-2026-5970](https://pkg.go.dev/vuln/GO-2026-5970) — an infinite loop on invalid input, reachable from the ent migration path.
- `PATCH /api/settings/*` now requires admin authorization — previously any authenticated user could change security-sensitive settings such as `auth.mode`, `git.allowPush`, or `worktree.force`.
- The agent bash allow-list filter now also rejects output redirection (`>`/`>>`/`<`), single-`&` backgrounding, and newline command separators, closing allow-list-widening patterns that could append to shell startup files or chain an extra command past the first-token check.
- Provider session discovery now skips symbolic links, so a crafted symlink placed under a provider directory can no longer be read as a session file (arbitrary-file-read guard).
- The dashboard-channel plugin now validates the `dashboard_reply` message argument and caps inbound `POST /message` bodies (64 KiB).

### Changed

- Remaining German UI strings in `ProjectSettings` and `RefinementChat` translated to English.
- `AgentModal` is now lazy-loaded, reducing the initial JS bundle size.
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
- Terminal worktree cleanup no longer force-removes a finished task's worktree
  when its branch still holds unpushed commits or uncommitted changes. Previously,
  if git-push was disabled (no `DASHBOARD_ALLOW_GIT_PUSH=true`) or a push failed,
  finalization committed only locally and the subsequent cleanup discarded the
  branch — orphaning the work as dangling commits. Cleanup now detects unpushed
  work and retains the worktree (audited as `worktree_retained_unpushed`) instead.
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
