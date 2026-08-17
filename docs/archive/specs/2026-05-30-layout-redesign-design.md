# Layout & Visual Redesign — Agent Overview

**Date:** 2026-05-30
**Status:** Approved design → implementation planning
**Scope:** Full UI redesign — app shell, navigation, status bar, visual language, and a calm/Linear-style restyle of **every** component (cards, tables, modals, settings, D3 visualizations).
**Out of scope:** Net-new functionality (no new features, endpoints, or data models). Backend changes only where a layout demands a trivial read (none currently anticipated).

---

## 1. Motivation

The current `App.vue` shell has two structural problems the user explicitly disliked:

1. **Overloaded header** (App.vue:212–358): brand + 4 stat badges + quota bar + search + a 4-way view toggle + 6 action buttons in one `flex-wrap` row → wraps below ~1400px, no visual hierarchy, ~14 equal-weight controls.
2. **Chrome stacking** (App.vue:360–419): five full-width strips before any content — `ResourceBar`, `SystemMetricsPanel`, `CostTrend`, channel-script bar, secondary view toolbar. Consumes 120–160px of vertical space and buries the agent grid.

Additionally: **`ResourceBar.vue` and `SystemMetricsPanel.vue` are duplicates** — both render identical CPU/MEM/DISK/LOAD bars from system info (one via `useSystemResources()`, the other re-polling `/api/system` every 15s). Dead duplication + double network.

Research (Vercel, Datadog, Grafana, Sentry, Linear changelogs, 2024–2026) converges on the same fix: **move view navigation into a left sidebar, strip the header to three elements, and turn always-on metric strips into collapsible/disclosure panels.** Full report archived in session notes; key citations inline below.

---

## 2. Decisions (locked with user)

| # | Decision | Choice |
|---|---|---|
| D1 | Shell structure | Icon-rail + collapsible sidebar (combination of sidebar patterns) |
| D2 | Sidebar expand trigger | Hover-peek **and** pin button; persisted; `Cmd/Ctrl+B` |
| D3 | Nav grouping | Grouped: **Monitor** / **Build** + utility anchored bottom |
| D4 | Persistent metrics (quota, total cost/tokens) | Sidebar **footer** |
| D5 | Operational metrics (CPU/MEM/DISK + cost sparkline) | **Bottom status bar**, Symfony-web-debug-toolbar style (thin strip → expand-up panel → collapse to corner tab) |
| D6 | Visual language | **B — Calm / Linear**: deeper bg, low-contrast chrome, content lifts via subtle surface not hard borders, indigo accent, soft live-glow, generous 8px-grid spacing |
| D7 | Scope | **Everything visual/structural** — all components incl. modals + D3 viz. No new features. |

---

## 3. Architecture

### 3.1 Shell composition

Replace `App.vue`'s `header + strips + main` markup with a 3-region CSS grid shell. `App.vue` shrinks to orchestration (auth gate, data streams, modal hosting, view switch); all chrome moves into dedicated components.

```
AppShell (grid: [rail|sidebar] / topbar / content / statusbar)
├── AppSidebar          (left; rail 56px ↔ expanded 220px)
│   ├── NavItem × N     (grouped; live badge slot)
│   └── SidebarFooter   (quota + cost/tokens + Sessions/Settings/Theme)
├── AppTopbar           (48px; title · search · per-view primary CTA · Live/Offline)
├── <slot> content      (active view; optional view-toolbar at top)
└── AppStatusBar        (bottom; collapsible Symfony-style segments)
```

New components: `AppShell.vue`, `AppSidebar.vue`, `NavItem.vue`, `SidebarFooter.vue`, `AppTopbar.vue`, `AppStatusBar.vue`, `DashboardToolbar.vue`, `LivePulse.vue`, `SkeletonCard.vue`.

### 3.2 View-state model

Current overloaded `viewMode: 'cards'|'list'|'pipeline'|'config-explorer'|'workflows'|'cost-analytics'` splits into two orthogonal concerns:

- `activeView: 'dashboard'|'workflows'|'pipeline'|'cost'|'config'` — driven by sidebar nav.
- `dashboardLayout: 'cards'|'list'` — driven by `DashboardToolbar`, only meaningful when `activeView==='dashboard'`.

Cost analytics graduates from a Dashboard sub-toggle to a **top-level nav item** (`activeView==='cost'`). This is the canonical source-of-truth change; both live in `useAgents.ts` (or a small new `useViewState.ts`) and persist `activeView` + sidebar collapse state to `localStorage`.

> SSOT note (per `layer2-project-core.md`): the new `--accent` token and any shared layout constants live in exactly one place. Status-color classes already centralize in `src/utils/statusColors.ts` — reuse, do not fork.

### 3.3 Design-token layer (keystone — do first)

`src/styles/main.css` `@theme inline` + `:root`/`.dark` blocks are the cascade root. The calm palette is implemented purely by re-tuning existing CSS variables + adding accent tokens. Because every component already consumes `bg-app/bg-card/bg-raised/border-line/text-fg*`, retuning these variables restyles the whole app in one pass.

New / retuned tokens:

```css
/* accent (replaces scattered blue-600/blue-500 usages) */
--color-accent: var(--accent);
--color-accent-soft: var(--accent-soft);

:root /* light */ {
  --accent: var(--color-indigo-600);
  --accent-soft: var(--color-indigo-100);
  /* calmer surfaces/borders retuned here */
}
.dark {
  --app: #0b0e14;            /* deeper near-black slate */
  --card: #12151c;
  --raised: #1c212b;
  --line: #1c212b;           /* low-contrast chrome */
  --line-strong: #2a313e;
  --accent: var(--color-indigo-400);
  --accent-soft: color-mix(in oklch, var(--color-indigo-400) 14%, transparent);
}
```

A follow-up sweep replaces hard-coded `blue-600`/`blue-500`/`bg-blue-*` accent usages across components with `accent` utilities (grep-driven; ~30 components). Status semantics (green/amber/gray/red) stay but every usage must pair color with icon/label (WCAG 1.4.1).

---

## 4. Component-by-component plan

### 4.1 New shell components

- **AppSidebar** — `<nav aria-label="Primary">`. Rail/expanded via a `collapsed` ref (localStorage). Hover-peek: `@mouseenter`/`@mouseleave` temporarily expands when not pinned. Pin button toggles persistent state, swaps `«`/`»`, sets `aria-expanded`. Nav groups Monitor/Build rendered from a config array `[{view, icon, label, group, badge}]`. Active item `aria-current="page"` + indigo left-border + brighter text. Each item exposes a `badge` slot fed by SSE counts (`filteredAgents.length`, `tasks.length`).
- **SidebarFooter** — quota progress bar (moves out of header, same `quotaPct`/`quotaSeverity` logic from App.vue:173–193), compact `Cost $X · Y tok`, then Sessions / Settings / Theme icon buttons (44px hit targets retained).
- **AppTopbar** — view title (from `activeView` → label map), search input (expand-on-focus 200→260px retained; `Cmd+K` opens existing `SpotlightSearch`), per-view primary CTA slot, `OfflineBadge` + new `LivePulse`.
- **AppStatusBar** — segments: System (CPU/MEM/DISK/LOAD) and Cost (3-min sparkline + Δ). Default thin strip; each segment is a button (`aria-expanded`) that expands an upward panel with detail (full metrics, cost history). A collapse control reduces the whole bar to a corner tab (state in localStorage). Sources data from `useSystemResources` + `costTrend` — **`SystemMetricsPanel.vue` is deleted**, its polling subsumed.
- **DashboardToolbar** — Cards/List segmented control + "Claude only" filter, rendered only inside the Dashboard view header. Lighter weight (`text-sm`, muted) to read as a view-level control.
- **LivePulse** — SSE connection indicator. `animate-pulse motion-reduce:animate-none motion-reduce:opacity-60`; static filled dot + `aria-label` under reduced motion. Amber "Reconnecting…" variant on SSE drop.
- **SkeletonCard** — shimmer block matching `AgentCard` proportions; replaces `Loading agents…` text. Reused as shimmer columns for Kanban initial load.

### 4.2 Restyle pass (calm language, every component)

Grouped for parallel subagent execution. Each task: apply token-based calm surfaces, 8px spacing rhythm, indigo accent, status color+icon pairing, reduced-motion safety. No behavior change unless noted.

- **Agent surfaces:** `AgentCard`, `AgentCardGrid`, `AgentTable`, `AgentRow`, `SubAgentRow`, `SubAgentList`, `EmptyAgentState`, `MachineBadge`, `ProviderBadge`, `OfflineBadge`, `ToolTimeline`.
- **Modals (restyle + decompose where large):**
  - `TaskModal.vue` (50KB) — **decompose** into header / stage-output / chat / feedback / permissions sub-components, then restyle. Largest single task.
  - `AgentModal.vue` — restyle; extract transcript/timeline sections if it eases the calm pass.
  - `SessionDetailModal`, `EditGateModal`, `AppModal` (base) + `ui/` primitives (`AppButton`, `AppBadge`, `AppCard`, `AppInput`, `AppSelect`, `AppModal`) — primitives restyled **first** within this group since others inherit them.
- **Pipeline/Build views:** `PipelineBoard`, `TaskCard`, `KanbanBoard`, `StageOutputView`, `StageCostWaterfall`, `AgentChatStream`, `RefinementChat`, `BacklogForm` + `backlog/` steps, `CrossLinkBanner`, `DependencyGraph`, `ExecutionWaterfall`, `PermissionTemplatePicker`, `TaskSlashCommandMenu`, `PromptInput`, `TaskList`.
- **Cost/analytics:** `CostAnalyticsView`, `CostTrend`, `CostForecast`, `CostHeatmap`.
- **Config/settings:** `ConfigExplorer`, `MemoryBrowser`, `ApiKeySettings` (38KB), `SpawnerSettings` (21KB), `ProjectSettings` (19KB), `NotificationSettings`, `SystemPromptSettings`, `PluginSettings`, `RemoteSettings`, `QuickCreateProjectPanel`, `AuditLogTab`.
- **Workflows / D3 visualizations:** `WorkflowsView`, `visualizations/SankeyChart`, `SessionDagChart`, `SpawnTreeChart`, `CoOccurrenceMatrix` — recolor node/edge/scale palettes to the calm tokens (D3 reads CSS vars or a shared JS palette constant; introduce one if absent — SSOT).
- **Misc:** `SpawnDialog`, `SpotlightSearch`, `SessionList`, `LoginPage`, `ResourceBar` (folded into AppStatusBar; component retired or repurposed), `WorktreePanel`/`WorktreePill`/`WorktreeCommandRunner`, `GitStatusPanel`, `SystemMetricsPanel` (**deleted**).

### 4.3 App.vue slim-down

`App.vue` keeps: auth gate, stream start, modal hosting, `navigateTo`, toast, quota fetch. Its template becomes `<AppShell>` with the active view in the default slot and modals as before. The channel-script bar + Install button move into Config/Settings.

---

## 5. Responsive behavior

| Breakpoint | Sidebar | Status bar |
|---|---|---|
| ≥1280px | expanded or pinned-collapsed (user pref) | full strip |
| 1024–1279px | icon rail, tooltips | full strip |
| 768–1023px | off-canvas drawer (hamburger in topbar) | collapsed to tab |
| <768px | full-screen drawer | collapsed to tab |

Sidebar collapse + status-bar state persist in `localStorage`.

---

## 6. Accessibility (WCAG 2.2)

- `<nav aria-label="Primary">`, `aria-current="page"` on active item, `aria-expanded`/`aria-controls` on sidebar + status-bar toggles, `aria-label` on every icon-only control, decorative glyphs `aria-hidden`.
- Skip-to-content link retained (App.vue:208) → `#main-content` with `tabindex="-1"`.
- On `activeView` change: `nextTick(() => mainEl.focus())` to announce the new view.
- `scroll-margin-top: 48px` on focusable content (sticky topbar, SC 2.4.11).
- Status colors always paired with icon/label (SC 1.4.1); verify ≥4.5:1 text contrast on the new deeper-dark surfaces.
- All motion wrapped in `motion-reduce:` variants; live indicators keep a static information-bearing fallback.

---

## 7. Testing

- **Unit (Vitest):** view-state split (`activeView`/`dashboardLayout` persistence), sidebar collapse/pin logic, status-bar expand state, `LivePulse` reduced-motion branch. Existing component tests updated to new markup.
- **E2E (Playwright):** nav switches view + focus moves; sidebar pin persists across reload; status-bar segment expands; `Cmd+B` toggles; `Cmd+K` opens spotlight; keyboard-only traversal of shell; reduced-motion media emulation.
- **A11y:** axe-core pass on shell + each view (per `playwright-best-practices`).
- Visual sanity: dark + light themes both render via the retuned tokens.

---

## 8. Build sequence (for the implementation plan)

1. **Design tokens** (`main.css`) — calm palette + `--accent`. Keystone; everything else reads from it.
2. **`ui/` primitives** restyle (AppButton/Badge/Card/Input/Select/Modal) — downstream components inherit.
3. **Shell components** (AppShell, AppSidebar, NavItem, SidebarFooter, AppTopbar, AppStatusBar, DashboardToolbar, LivePulse, SkeletonCard) + view-state split + `App.vue` slim-down. Delete `SystemMetricsPanel`. **This is the milestone that fixes the user's stated complaint** — verify before fan-out.
4. **Restyle fan-out** (subagent-driven, parallel by group 4.2): agent surfaces · pipeline · cost · config/settings · workflows-D3 · misc.
5. **TaskModal decomposition** (own task, largest).
6. **Accent grep-sweep** — replace residual `blue-*` accent classes with `accent`.
7. **Tests + axe + responsive QA.**

Steps 1–3 are sequential (each gates the next). Step 4 groups run in parallel. Steps 5–7 follow.

---

## 9. Risks

- **Scope size** — ~60 components. Mitigated by token-first cascade (most "restyles" become near-free once tokens land) and parallel fan-out.
- **TaskModal decomposition** could touch behavior — treat as isolated task with its own test pass before/after.
- **D3 palettes** may hard-code colors — introduce a single shared palette constant (SSOT) rather than scattering hex.
- **Light theme** must be validated alongside dark — same token source, but contrast differs.
