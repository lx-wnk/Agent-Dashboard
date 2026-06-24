# Activity Log

<!-- Append-only. Do NOT edit or delete existing entries. -->
<!-- Format: - (YYYY-MM-DD) [action] — [context/outcome] -->
<!-- Examples: -->
<!-- - (2026-01-15) Refactored auth module — moved session logic to SessionService -->
<!-- - (2026-01-16) User confirmed: no mocking in integration tests -->

- (2026-06-23) anthropic-spawner shipped — out-of-process Anthropic Messages API binary added as adapter_type "anthropic"; CI matrices updated; docs (README, CHANGELOG, CONTRIBUTING, PRIVACY, configuration.md) and ADR-0008 recorded
- (2026-05-22) G1 (VA-2) shipped — PR #65 opened (`feat/va2-workflow-visualizations` → `upcoming`). All 14 tasks landed: 4 D3 charts (Sankey/DAG/spawn-tree/co-occurrence) + analytics builders + 4 endpoints + view-toggle. Deviations: full-file scanner in `analytics/scan.go` instead of extending `parser.ParseSessionFile` (which tail-reads 32KB); `SpawnTreeNode.CostCents` stubbed to 0 (cost integration parked). 200/200 vitest pass, all Go tests green, 3 typecheck errors pre-existing on `upcoming`.
- (2026-05-22) G3 (CI-8) cancelled — `upcoming` already has `server/internal/api/system/quota.go` reading `~/.claude/usage-data/*.json` + `App.vue` quota chip with severity tiers. Roadmap audit grep missed it (searched for `quota_period|quotaUsage` — actual code uses `tokensUsed|periodEnd`). Spec retained as alt-impl reference.
- (2026-05-22) G2 (IP-1) cancelled — `upcoming` already has `useAgentIdentity.ts` + COLORS/EMOJIS palette + AgentRow/Card/Modal integration. Original roadmap audit missed it because components are inline (not `Identity*`-prefixed). Only `IdentityEditor.vue` (explicit edit UI from spec §C.3) is missing — left as optional follow-up.
- (2026-05-22) Roadmap audit + VA-2 spec/plan written — todo.md was stale (4 pipeline tasks all done); 3 unbuilt roadmap items confirmed: VA-2 (P2), IP-1 (P3), CI-8 (P3); wrote `docs/superpowers/specs/2026-05-22-d3-workflow-visualizations-design.md` + `docs/superpowers/plans/2026-05-22-d3-workflow-visualizations.md`
- (2026-05-17) Gap resolution complete on feat/gap-resolution — TodoWrite task extraction (parser.go), graceful-shutdown ctx fix (di.go), MAX_DESCRIPTION_CHARS SSOT, refine handler tests, LLM Adapter Settings UI, Plugin Status UI + GET /api/plugins, Notification Settings UI; all tests green (Go + Vitest 66 tests)
- (2026-05-12) Go rework complete through Phase 6+ — MCP (19 tools, 4 scopes), channel bridge, refinement chat, history import, web push, FTS5 search, remote registrations, permission presets all implemented on feat/go-rework
- (2026-05-12) docs: README + CONTRIBUTING rewritten for Go backend; outputs/ removed from git history via filter-repo + added to .gitignore; PR #29 (feat/go-rework → upcoming) updated
- (2026-06-22) Provider registry backend (Codex/Gemini/Junie opt-in + Ollama $0) shipped on feat/provider-integration — declarative YAML descriptors, dispatch moved out of parser, CostLocal field for local models.
- (2026-06-22) Provider settings UI + DB persistence shipped on feat/provider-integration — per-provider enable toggle (Settings → Providers), provider_setting ent table, GET/PATCH /api/providers endpoints, live enable snapshot updated within one scan tick.
