# Current Tasks

> All base off `upcoming` branch, work in worktrees.

## Open Roadmap Audit (2026-06-04)

Full triangulation (inline TODOs / GH issues / GH PRs / 36 roadmap IDs / 6 plans 05-19→06-01) → project is ~98% feature-complete. All 6 recent plans shipped in `6ec0bbb` (#114). Only **2 genuinely open items**, both token/cost correctness — specs generated, ready to implement:

| ID | Status | Gap | Spec |
|---|---|---|---|
| CI-7 Agent health/anomaly score | PARTIAL | UI (`AgentCard.vue:57`) + `sdk.Agent.HealthScore` exist; `merger/` never computes it → always `0`. Formula: success 40% / cache-hit 25% / error 25% / cost-spike 10% | `docs/superpowers/specs/2026-06-04-ci7-agent-health-score-design.md` |
| CI-4 Compaction-aware token baseline | OPEN | Compaction (`type:system, subtype:compact_boundary`, `compactMetadata.{preTokens,postTokens}`) resets cumulative tokens; 32KB tail-read misses markers (>300KB files). Real session: 100% under-count (31/7025 vs true 21598/1856151) | `docs/superpowers/specs/2026-06-04-ci4-compaction-token-baseline-design.md` |

Spec key decisions: CI-7 → new `merger/health.go`, 7-day cost baseline injected via `agentbroadcast/loop.go` (layer-safe), error-state hard-caps score ≤30. CI-4 → recommend Option B full-file scan on cache-miss, all changes confined to `parser.go`, ~3.0 PD.
Next: implement via subagent-driven-development (one worktree per item, zero file overlap — CI-7 touches merger+agentbroadcast, CI-4 touches parser only).

## Active — Parallel Groups (dispatched 2026-05-22)

| Group | Roadmap | Spec | Plan | Branch | Status |
|---|---|---|---|---|---|
| G1 — D3 Workflow Viz | VA-2 (P2) | `docs/superpowers/specs/2026-05-22-d3-workflow-visualizations-design.md` | `docs/superpowers/plans/2026-05-22-d3-workflow-visualizations.md` | `feat/va2-workflow-visualizations` | ✅ PR #65 open — all 14 tasks shipped, +2567/-61, lint+tests green. https://github.com/lx-wnk/Agent-Dashboard/pull/65 |
| G2 — Agent Identity | IP-1 (P3) | `docs/superpowers/specs/2026-05-22-per-agent-color-emoji-identity-design.md` | `docs/superpowers/plans/2026-05-22-per-agent-color-emoji-identity.md` | `feat/ip1-agent-identity` | CANCELLED 2026-05-22 — already shipped in `upcoming` (`src/composables/useAgentIdentity.ts` + integration in AgentRow/AgentCard/AgentModal). Only optional follow-up: explicit `IdentityEditor.vue` UI (spec §C.3). Roadmap audit missed it because components are inline, not prefixed `Identity*`. |
| G3 — Quota Tracking | CI-8 (P3) | `docs/superpowers/specs/2026-05-22-claude-quota-tracking-design.md` | `docs/superpowers/plans/2026-05-22-claude-quota-tracking.md` | `feat/ci8-quota-tracking` | CANCELLED 2026-05-22 — already shipped via simpler impl: `server/internal/api/system/quota.go` reads `~/.claude/usage-data/*.json` directly; `App.vue` renders header chip with severity tiers. Heavier spec version kept as alt-impl reference. |

Independence: zero file overlap.
- G1 touches `server/internal/{analytics,api/visualizations,api/router.go,cmd/serve/di.go}`, `src/components/{WorkflowsView,visualizations/*}.vue`, `src/composables/useWorkflows.ts`, `src/App.vue` (view-toggle entry), `sdk/types.go`, `src/sdk.generated.ts`, `package.json` (d3-sankey).
- G2 touches `src/composables/useAgentIdentity.ts`, `src/utils/agentIdentityPalette.ts`, `src/components/{Identity*,AgentRow,AgentCard,AgentModal,SubAgentRow,TaskCard}.vue`.
- G3 touches `server/internal/{quota,api/quota,api/router.go,cmd/serve/di.go}`, `src/composables/useQuota.ts`, `src/components/QuotaChip.vue`, `src/App.vue` (header chip mount), `sdk/types.go`, `src/sdk.generated.ts`, `.agent-context/conventions.md`.

Collision points: `src/App.vue`, `server/internal/api/router.go`, `server/cmd/serve/di.go`, `sdk/types.go`, `src/sdk.generated.ts` — resolved at PR-merge time (small diffs, easy to rebase).

## Pipeline Tasks (grouped, 2026-05-20) — COMPLETE

| Slug | DB ID | Groups | Final status |
|---|---|---|---|
| `quality-security-ci` | 90572a73 | T-01 + T-02 + T-03 | done |
| `quick-wins-multi-provider-metrics` | afd96cdb | T-04 | done |
| `cost-analytics-quota` | 09c57f02 | T-05 + T-08 | done |
| `developer-tooling-worktree-config` | 10f69b71 | T-06 + T-07 | done |

## Completed (recent)

- 2026-05-22: spawn dialog project picker (#63), spawner dispatch (#63), settings consolidation (#64)
- 2026-05-21: 4 pipeline tasks finalized
- 2026-05-20: Fixed FTS5 migration bug; refinement confirm endpoint fix (d354159)
- 2026-05-19: PRs #44–60 merged into `upcoming` — all P1/P2/P3 findings resolved
