# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

From the first tagged release onward, release notes are generated automatically
from [Conventional Commits](https://www.conventionalcommits.org/) by GoReleaser.

## [Unreleased]

Preparing the first public release.

### Added

- `anthropic` spawner adapter — run pipeline stage agents and refinement chat against the Anthropic Messages API via an out-of-process binary, keeping the SDK out of the server module. Requires `ANTHROPIC_API_KEY` in the server env and the `anthropic-spawner` binary on `PATH` (or `DASHBOARD_ANTHROPIC_SPAWNER_CMD`). Default model: `claude-opus-4-8`.
- Documented using the existing `openai` adapter with OpenAI-compatible multi-model gateways (OpenRouter, Together AI, Inflection, …) — set `base_url` + `api_key_env` + `default_model`, no new code needed.
- Collapsible agent groups — when a grouping (project/status/model) is active on the
  dashboard roster, click a group header to collapse or expand it. On first load only
  the highest-priority non-empty group is expanded (Active, else Waiting, else Idle,
  else Finished); the rest start collapsed. Collapsed state is remembered per grouping
  mode in localStorage and survives live re-renders.
- Opt-in monitoring of Codex CLI, Gemini CLI, Junie CLI, and pi.dev agents via a declarative provider registry (off by default; enable with DASHBOARD_PROVIDERS_ENABLED or custom descriptors in DASHBOARD_PROVIDER_DIR). pi.dev's per-session JSONL (`~/.pi/agent/sessions/`) carries token usage, model, provider, and cost per turn.
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

### Changed

- Agent card redesign — prominent, readable project name; a compact
  cost · tokens · uptime metric row with the full labeled detail (last activity,
  burn rate, cache costs) moved into a hover ⓘ popover and the agent modal; the
  prompt input is now always docked at the bottom with a larger output area.
- Accessibility: clickable agent rows are now native `<button>` elements, and the
  agent-modal summary uses a higher-contrast token.
- SSE poll and retry intervals are centralized in `src/utils/sse.ts` instead of
  being hard-coded at call sites.

### Removed

- Pruned unused TS-era dependencies never imported by the shipped app: `express`,
  `nodemailer`, `web-push`, `cookie-parser`, `supertest`, and their `@types/*`.

### Fixed

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

<!-- ofd-smoke -->