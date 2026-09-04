# Cockpit Shell and the GitHub Application Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship S1 (the OS shell) as a cockpit landing view, and the system's second `reach` application — GitHub — through the same registry, capability gate and encrypted-settings path Obsidian uses, with no kernel change specific to either. That "no kernel change" outcome is the MLP entry criterion, and it is what this plan is really testing.

**Architecture:** `server/internal/apps/github/` copies the Obsidian application shape exactly — `app.go` writes one registry row and four capability rows on every boot (idempotent), `client.go` talks to the GitHub REST API and enforces nothing. Two surfaces reach it and both gate: four HTTP routes under `/api/github/*` and four MCP tools. The repo allow-list is parsed once into the client and checked by the callers *before* `Gate.Authorize`, so the string the gate rules on and the string the client acts on are the same `owner/name`. On the client, `ActiveView` gains `'cockpit'` and becomes the default; the existing dashboard branch moves out of `App.vue` into `src/features/cockpit/` first, unchanged, so the one step that can break something that works today lands alone.

**Tech Stack:** Go 1.26 (chi, ent ORM, modernc/sqlite, cobra), Vue 3 + TypeScript SPA (Vite, pnpm, Vitest, Vue Test Utils, Playwright)

**Spec:** `docs/superpowers/specs/2026-09-03-cockpit-and-github-application-design.md` (frame: `docs/superpowers/specs/2026-08-27-agenticos-overview-design.md`)

## Global Constraints

- **Server MUST bind to `127.0.0.1`.** Never `0.0.0.0`. Nothing in this plan opens a listener; do not change any bind address while working in `server/`.
- **Never run `go test ./...` or `task test` while implementing.** Both regenerate `server/internal/db/ent/`, which then shows up as unrelated noise in the diff. Scope every Go test run to the package under change. If the tree is already dirty under `server/internal/db/ent/`, restore it with `git checkout -- server/internal/db/ent/`.
- **No task in this plan regenerates ent.** There is no schema change here: the GitHub application writes into the existing `resource`, `capability` and `app_setting` tables through their existing repos. If you find yourself editing `server/internal/db/ent/schema/`, stop — you have left the plan. Should a regeneration happen by accident, the recovery path is `cd server && go generate ./internal/db/ent/` (it carries `--feature sql/upsert`; verify with `grep -rl "OnConflict" server/internal/db/ent/ | head`, which must print files), then `git checkout -- server/go.sum` and, if the workspace sum moved, `git checkout -- go.work.sum` — `go generate` pulls codegen-only dependencies into both that `go build` does not need. Also `git checkout -- server/internal/db/ent/runtime/runtime.go` if it lost its `Version`/`Sum` constants.
- **`gofmt -l <pkg>` is mandatory before every Go commit.** CI runs `golangci-lint fmt --diff`, which fails on struct-field and comment alignment that `go build`, `go vet` and `go test` all accept. A green build is not evidence of a green CI. `gofmt -l` printing nothing is the pass.
- **Run `go vet ./...` module-wide (from `server/`) before every Go commit.** A package-scoped `go test` misses `_test.go` files in parent or sibling packages that reference a changed exported type, and `go build` skips test files entirely.
- **Lint with the CI toolchain:** `GOTOOLCHAIN=go1.26.6 task lint`. A locally newer Go produces hundreds of phantom `typecheck` errors that do not exist in CI.
- **Frontend gate:** `pnpm lint && pnpm typecheck && pnpm test`. Paste the raw output. A summary is not evidence.
- **`pnpm build` wipes `server/frontend/dist/.gitkeep`**, which `//go:embed all:dist` in `server/frontend/embed.go` needs to compile without a frontend build. If any step runs a build, restore it with `git checkout HEAD -- server/frontend/dist/.gitkeep` before committing.
- **Adding a route breaks the route golden.** `server/internal/api/testdata/routes.golden` is asserted by `server/internal/api/route_golden_test.go`. Task 6 adds four lines to it deliberately; regenerate with `cd server && go test -count=1 ./internal/api/ -run TestRouteGolden -update-golden` and review the diff — exactly four added lines are expected.
- **Cross-feature deep imports are an ESLint error.** `eslint.config.js:49-54` registers a local `boundary/feature-internals` rule: a file under `src/features/<a>/` may not import `@/features/<b>/components/...` or `@/features/<b>/composables/...`, only the barrel `@/features/<b>`. `src/App.vue` is *not* under `src/features/`, so its existing deep imports are legal — moving code out of `App.vue` into a feature is exactly when this rule starts to bite. Task 1 handles it by extending `src/features/agents/index.ts`.
- **Never add a manual `unmount()` to a component test.** `src/test/setup.ts` calls `enableAutoUnmount(afterEach)` globally. Wrappers are torn down for you.
- **The GitHub token MUST never be logged and MUST never be returned by any read endpoint or MCP tool.** `settings.Definition{Secret: true}` masks it on every read that is not `Service.Secret`; nothing in this plan may add a second reader.
- **All code, comments, commit messages, PR titles and bodies in English.** Conventional Commits (`feat:`, `fix:`, `refactor:`, `docs:`), one commit per task, no task or phase numbers in the message.

---

## Spec claims checked against the code before planning

Every claim the spec makes about existing code was opened and read. Three did not survive.

| Spec claim | Verdict | Evidence |
|---|---|---|
| `defaultEffect` sends `spend` → deny, `reach`/`tool`/`resource` → ask, unknown → deny (`decide.go:233-242`) | **VERIFIED** | `server/internal/capability/decide.go:233-242` — exactly that switch, and the line numbers are right |
| `Reversible` is written to the catalogue and read by nothing | **VERIFIED** | `capability.CapabilityView` (`decide.go:60-64`) carries only `Name`, `Class`, `EnforceableBy`; `Decide` never mentions reversibility; `repo.UpsertCapabilityInput` (`capability_repo.go:34-41`) does persist it, and `apps/obsidian/app_test.go:22-46` asserts the stored value |
| `Register` writes the registry row plus capability rows, idempotent on every boot | **VERIFIED** | `server/internal/apps/obsidian/app.go:66-91` — one `resources.Upsert`, then one `caps.Upsert` per decl, both resolving on conflict |
| `buildObsidianClient` fails the boot on half-configuration, naming the missing keys | **VERIFIED** | `server/serverapp/di_obsidian.go:25-61` — all-three-empty returns `nil, nil`; otherwise a `missing []string` is accumulated and joined into the error |
| MCP tools authorize "through `memory.Gate` with the caller contexts `CallerResolver` supplies" (§4.2) | **FALSE on this branch** | `grep -rn 'CallerResolver' server/` returns nothing. `CallerResolver` is introduced by the unmerged sibling branch `feat/stage-run-mcp-credentials`. Today every tool calls `d.Gate.Authorize(ctx, cap, value, scope)` with no extra contexts — `server/internal/mcp/tools/obsidian.go:77, 116, 155, 187`. **Plan against the code:** the GitHub tools call `Authorize` with four arguments, exactly like `obsidian.go`. The variadic `extra ...capability.Context` already exists (`server/internal/memory/authorize.go:99`), so when `CallerResolver` lands the GitHub tools get contexts by the same one-line edit the Obsidian tools will get. |
| `github.token`/`github.repos`/`github.baseURL` are a required **trio**, half-configuration fails the boot (§4.1) | **PARTLY FALSE as stated** | A `settings.Definition` with a `Default` is never unset — `registry.go:38-42` even forbids a `Default` on a `Secret` definition. `github.baseURL` needs a default (`https://api.github.com`, §4.1's own table) and therefore cannot be half of anything. **Plan against the code:** the required set is a **pair**, `github.token` + `github.repos`. Both empty = the application is off; both set = on; exactly one set = the boot fails naming the missing key. `github.baseURL` is validated (parseable absolute URL) but never counted. |
| §4.2 lists `GET /api/github/summary` as the HTTP surface, while the brief's rule is "every gated action is reachable over HTTP **and** as an MCP tool" | **Spec is narrower than the rule it states** | With one HTTP route and four MCP tools, `github.comment` and `github.merge` would be MCP-only — the exact one-surface shape §4.2 exists to forbid. **Decision:** four HTTP routes, one per capability, `GET /api/github/summary` among them by name. Task 6 adds a parity test that fails if the two surfaces ever diverge. |
| `App.vue` is 473 lines and carries the view branching inline | **VERIFIED** | `wc -l src/App.vue` = 473 |
| `ActiveView` is `'dashboard' \| 'workflows' \| 'pipeline' \| 'cost' \| 'schedules' \| 'eval'` at `useViewState.ts:5` | **VERIFIED** | exact line, exact union |

Two further facts the spec does not mention but the plan depends on, both verified:

- **`useAgents` and `useTasks` are module-level singletons** (`src/features/agents/composables/useAgents.ts:22-28`, `src/features/pipeline/composables/useTasks.ts:9-12` — `shallowRef`/`ref` at module scope). Calling `useAgents({ autoStart: false })` from a second component returns the *same* refs, so the moved dashboard needs no agent props at all.
- **`usePendingPermissions` is NOT a singleton** (`src/composables/usePendingPermissions.ts:19` builds a fresh `cache` per call). Calling it a second time would duplicate every permission fetch. `App.vue` keeps owning it, and `permissionItems` crosses into the moved component as a prop.

---

## File Structure

| File | Responsibility |
|---|---|
| `src/features/cockpit/components/DashboardView.vue` | The agent-roster view, moved verbatim out of `App.vue` |
| `src/features/cockpit/components/DashboardView.test.ts` | Pins that the moved view renders the same toolbar, roster and empty state |
| `src/features/agents/index.ts` | Public barrel — gains the four roster components the cockpit feature now imports |
| `server/internal/apps/github/app.go` | Slug, the four capability declarations and their classes, `Register` |
| `server/internal/apps/github/app_test.go` | Class/reversibility of each capability; `Register` idempotence |
| `server/internal/apps/github/client.go` | Repo allow-list parsing and the four API calls. Enforces no capability |
| `server/internal/apps/github/client_test.go` | Allow-list refusal, request shape, token never echoed |
| `server/internal/settings/registry.go` | The three `github.*` definitions, `github.token` secret |
| `server/serverapp/di_github.go` | `buildGitHubClient` — the pair rule, and the boot failure that names the missing key |
| `server/serverapp/di_github_test.go` | Off, on, and each half-configured direction |
| `server/serverapp/di.go` | `github.Register` on boot; client construction; handler and MCP wiring |
| `server/internal/api/github/handler.go` | Four HTTP routes, each: allow-list check → gate → client |
| `server/internal/api/github/handler_test.go` | Per-route deny/allow, allow-list-before-gate, no token in any response |
| `server/internal/api/router.go` | Mounts the GitHub handler in the session-authenticated group |
| `server/internal/api/testdata/routes.golden` | Four new route lines |
| `server/internal/mcp/tools/github.go` | Four MCP tools, same order of checks as the HTTP handler |
| `server/internal/mcp/tools/github_test.go` | Per-tool deny/allow, allow-list-before-gate, and the two-surface parity test |
| `server/internal/mcp/auth.go` | `ToolScopeMap` and `scopeImplies` entries for the three GitHub scopes |
| `server/internal/mcp/tools/keys.go` | `validKeyScopes` entries for the three GitHub scopes |
| `src/composables/useViewState.ts` | `'cockpit'` in `ActiveView` and `ACTIVE_VIEWS`; the new default |
| `src/utils/navConfig.ts` | The Cockpit nav item |
| `src/features/cockpit/components/CockpitPanel.vue` | The panel shell — the single owner of the five mutually exclusive states |
| `src/features/cockpit/panelState.ts` | `PanelState` union and the testid convention |
| `src/features/cockpit/components/CockpitView.vue` | Composes the five panels |
| `src/features/cockpit/components/AgentsPanel.vue` | Live agent roster summary |
| `src/features/cockpit/components/PipelinePanel.vue` | Task counts by stage |
| `src/features/cockpit/components/RoutinesPanel.vue` | `GET /api/resources?kind=routine` |
| `src/features/cockpit/components/MemoryPanel.vue` | `GET /api/resources?kind=memory_space`, gated on `memory.read` |
| `src/features/cockpit/components/GitHubPanel.vue` | `GET /api/github/summary` |
| `src/features/cockpit/composables/useGitHubSummary.ts` | The panel's fetch and its state machine |
| `src/features/settings/components/GitHubSettings.vue` | Token / repos / base URL form, pair rule enforced client-side |
| `tests/e2e/cockpit.spec.ts` | The five states, per panel, each distinguishable |
| `README.md`, `CHANGELOG.md`, `docs/guides/mcp.md`, `docs/guides/security.md` | Documentation, in the same change |

---

### Task 1: Move the dashboard branch into `features/cockpit/`, with no behaviour change

**Why first.** Spec §4.4 step 1 and D5: this is the only step that can break something that works today, so it lands alone, where a failure is unambiguous.

**Files:**
- Create: `src/features/cockpit/components/DashboardView.vue`
- Create: `src/features/cockpit/components/DashboardView.test.ts`
- Modify: `src/features/agents/index.ts`
- Modify: `src/App.vue`

**Interfaces:**
- Consumes: `useAgents({ autoStart: false })`, `useViewState()`, `useSpawners()`, `useNow()` (all module-level singletons — see the verification table), `PermissionItem` from `@/composables/usePendingPermissions`.
- Produces:
  - `src/features/agents/index.ts` additionally exports `AgentCardGrid`, `AgentTable`, `AgentTriageBand`, `EmptyAgentState`.
  - `DashboardView.vue` props: `{ permissionItems: PermissionItem[]; focusedSessionId: string | null }`
  - `DashboardView.vue` emits: `approve: [taskId: string, ids: string[], remember: boolean]`, `deny: [taskId: string, ids: string[]]`

**Two things deliberately stay in `App.vue`, and both are behaviour-preservation:**

1. The `v-if="isLoading && activeView === 'dashboard'"` skeleton branch and the `v-else-if="error"` branch. Today the chain is skeleton → error → dashboard, so when the stream errors before its first payload (`isLoading` and `error` both true) the skeletons win. Moving the skeleton into `DashboardView` would let the error branch win instead — a real, reachable behaviour change. Only the `<template v-else-if="activeView === 'dashboard'">` body moves.
2. The keyboard handler. `handleKeydown` reads `focusedSessionId`, `attentionAgents` **and** `App.vue`-owned overlay state (`selectedAgent`, `showSettings`, `showSpawnDialog`, `showBacklogForm`, `showSessions`, `showRefinementChat`, `activeConceptTask`, `showPlanReview`, `activePlanTask`). Splitting it would either duplicate that overlay list or export it. `focusedSessionId` stays an `App.vue` ref and crosses as a prop.

`autoApprovingStrip` *does* move: `AutoApprovingStrip` is inside the moved branch and its ref is used only by the `@remembered` handler that also moves.

- [ ] **Step 1: Write the failing test**

Create `src/features/cockpit/components/DashboardView.test.ts`:

```ts
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import DashboardView from './DashboardView.vue'

// The roster reads the module-level singletons in useAgents/useViewState, so a
// test drives it by stubbing those modules rather than by passing props. Only
// the two genuinely per-caller values (permissionItems, focusedSessionId) are
// props — see usePendingPermissions, which is not a singleton.
vi.mock('@/features/agents', async () => {
  const { ref, shallowRef, computed } = await import('vue')
  const agents = shallowRef<any[]>([])
  return {
    useAgents: () => ({
      agents,
      filteredAgents: computed(() => agents.value),
      attentionAgents: computed(() => []),
      pendingCapabilityDecisions: ref([]),
      searchQuery: ref(''),
      selectAgent: vi.fn(),
      dismissAgent: vi.fn(),
    }),
    AgentCardGrid: { name: 'AgentCardGrid', template: '<div data-testid="agent-card-grid" />' },
    AgentTable: { name: 'AgentTable', template: '<div data-testid="agent-table" />' },
    AgentTriageBand: { name: 'AgentTriageBand', template: '<div data-testid="triage-band" />' },
    EmptyAgentState: { name: 'EmptyAgentState', template: '<div data-testid="empty-state" />' },
  }
})

describe('dashboardView', () => {
  it('renders the toolbar, the triage band and the empty state when no agent is live', () => {
    const wrapper = mount(DashboardView, {
      attachTo: document.body,
      props: { permissionItems: [], focusedSessionId: null },
      global: {
        stubs: {
          AutoApprovingStrip: { template: '<div data-testid="auto-approving-strip" />' },
          DashboardToolbar: { template: '<div data-testid="dashboard-toolbar" />' },
          ChannelScriptCallout: { template: '<div data-testid="channel-script-callout" />' },
        },
      },
    })

    expect(wrapper.find('[data-testid="triage-band"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="dashboard-toolbar"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="empty-state"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="agent-card-grid"]').exists()).toBe(false)
  })
})
```

- [ ] **Step 2: Run the test and watch it fail**

Run: `pnpm test src/features/cockpit/components/DashboardView.test.ts`
Expected: `Failed to resolve import "./DashboardView.vue"`. Any other failure means the mock is wrong, not the feature.

- [ ] **Step 3: Widen the agents barrel**

The moved file lives under `src/features/cockpit/`, so `@/features/agents/components/AgentCardGrid.vue` is now an ESLint error (`boundary/feature-internals`). Replace `src/features/agents/index.ts` with:

```ts
export { default as AgentCardGrid } from './components/AgentCardGrid.vue'
export { default as AgentChatStream } from './components/AgentChatStream.vue'
export { default as AgentTable } from './components/AgentTable.vue'
export { default as AgentTriageBand } from './components/AgentTriageBand.vue'
export { default as EmptyAgentState } from './components/EmptyAgentState.vue'
export { useAgentIdentity } from './composables/useAgentIdentity'
export * from './composables/useAgents'
```

- [ ] **Step 4: Create the moved view**

Create `src/features/cockpit/components/DashboardView.vue`. The `<template>` is the body of `App.vue`'s `<template v-else-if="activeView === 'dashboard'">`, copied character-for-character; the `<script setup>` is the subset of `App.vue`'s script the template needs.

```vue
<script setup lang="ts">
import type { PermissionItem } from '@/composables/usePendingPermissions'
import { computed, ref } from 'vue'
import AutoApprovingStrip from '@/components/AutoApprovingStrip.vue'
import ChannelScriptCallout from '@/components/shell/ChannelScriptCallout.vue'
import DashboardToolbar from '@/components/shell/DashboardToolbar.vue'
import { useNow } from '@/composables/useNow'
import { useSpawners } from '@/composables/useSpawners'
import { useViewState } from '@/composables/useViewState'
import { AgentCardGrid, AgentTable, AgentTriageBand, EmptyAgentState, useAgents } from '@/features/agents'
import { groupAgents, sortAgents } from '@/utils/agentGroup'
import { friendlyProjectName } from '@/utils/friendlyProjectName'

defineProps<{
  permissionItems: PermissionItem[]
  focusedSessionId: string | null
}>()
const emit = defineEmits<{
  approve: [taskId: string, ids: string[], remember: boolean]
  deny: [taskId: string, ids: string[]]
}>()

// autoStart: false, exactly as App.vue calls it — useAgents holds module-level
// state, so this is the same stream App.vue already started, not a second one.
const { agents, filteredAgents, attentionAgents, pendingCapabilityDecisions, searchQuery, selectAgent, dismissAgent } = useAgents({ autoStart: false })
const { dashboardLayout, dashboardSort, dashboardGroup, setDashboardGroup, dashboardProject, dashboardSpawner } = useViewState()
const { spawners } = useSpawners()
const { nowMs } = useNow()

const autoApprovingStrip = ref<InstanceType<typeof AutoApprovingStrip> | null>(null)

// Dashboard roster: project + spawner filter → sort → optional grouping. Project
// options list every known project (pre-filter) so the dropdown stays stable.
const rosterAgents = computed(() => {
  let base = filteredAgents.value
  if (dashboardProject.value !== 'all')
    base = base.filter(a => a.projectName === dashboardProject.value)
  if (dashboardSpawner.value !== 'all')
    base = base.filter(a => a.spawnerId === dashboardSpawner.value)
  return sortAgents(base, dashboardSort.value, nowMs.value)
})
const rosterGroups = computed(() => groupAgents(rosterAgents.value, dashboardGroup.value))
const projectOptions = computed(() => [
  { value: 'all', label: 'All projects' },
  ...[...new Set(agents.value.map(a => a.projectName))].sort().map(n => ({ value: n, label: friendlyProjectName(n) })),
])
const spawnerOptions = computed(() => [
  { value: 'all', label: 'All spawners' },
  ...spawners.value.map(s => ({ value: s.id, label: s.name })),
])

defineExpose({ rosterAgents })
</script>

<template>
  <AgentTriageBand
    :agents="attentionAgents"
    :permission-items="permissionItems"
    :capability-decisions="pendingCapabilityDecisions"
    :focused-session-id="focusedSessionId"
    @select="selectAgent"
    @remembered="autoApprovingStrip?.load()"
    @approve="(taskId, ids, remember) => emit('approve', taskId, ids, remember)"
    @deny="(taskId, ids) => emit('deny', taskId, ids)"
  />
  <AutoApprovingStrip ref="autoApprovingStrip" />
  <DashboardToolbar
    :layout="dashboardLayout"
    :spawner="dashboardSpawner"
    :project="dashboardProject"
    :sort-by="dashboardSort"
    :group-by="dashboardGroup"
    :search-query="searchQuery"
    :project-options="projectOptions"
    :spawner-options="spawnerOptions"
    :total-count="agents.length"
    :shown-count="rosterAgents.length"
    @update:layout="dashboardLayout = $event"
    @update:spawner="dashboardSpawner = $event"
    @update:project="dashboardProject = $event"
    @update:sort-by="dashboardSort = $event"
    @update:group-by="setDashboardGroup($event)"
    @update:search-query="searchQuery = $event"
  />
  <template v-if="dashboardLayout === 'list'">
    <EmptyAgentState v-if="rosterAgents.length === 0" :search-query="searchQuery" />
    <AgentTable v-else :agents="rosterAgents" :groups="rosterGroups" @select="selectAgent" />
  </template>
  <template v-else>
    <EmptyAgentState v-if="rosterAgents.length === 0" :search-query="searchQuery" />
    <AgentCardGrid v-else :agents="rosterAgents" :groups="rosterGroups" :group-by="dashboardGroup" @select="selectAgent" @dismiss="dismissAgent" />
  </template>
  <ChannelScriptCallout />
</template>
```

- [ ] **Step 5: Run the test and watch it pass**

Run: `pnpm test src/features/cockpit/components/DashboardView.test.ts`

- [ ] **Step 6: Cut the moved code out of `App.vue`**

In `src/App.vue`:

Replace the whole `<template v-else-if="activeView === 'dashboard'"> … </template>` block with:

```html
        <DashboardView
          v-else-if="activeView === 'dashboard'"
          :permission-items="permissionItems"
          :focused-session-id="focusedSessionId"
          @approve="(taskId, ids, remember) => approvePermission(taskId, ids, remember)"
          @deny="(taskId, ids) => denyPermission(taskId, ids)"
        />
```

Add the import next to the other feature imports:

```ts
import DashboardView from '@/features/cockpit/components/DashboardView.vue'
```

Delete from `App.vue`'s script, because nothing else references them any more:
`rosterAgents`, `rosterGroups`, `projectOptions`, `spawnerOptions`, `autoApprovingStrip`, `const { spawners } = useSpawners()`, `const { nowMs } = useNow()`, and the now-unused imports `AgentCardGrid`, `AgentTable`, `AgentTriageBand`, `EmptyAgentState`, `AutoApprovingStrip`, `DashboardToolbar`, `ChannelScriptCallout`, `useSpawners`, `useNow`, `groupAgents`, `sortAgents`, `friendlyProjectName`, and `dismissAgent`/`searchQuery`/`pendingCapabilityDecisions` from the `useAgents` destructure *only if* no other `App.vue` code reads them.

**Read before deleting:** `pendingCapabilityDecisions` is still read by `combinedAttentionCount`, and `attentionAgents` by the keyboard handler. Keep both. `dashboardLayout` is still read by the `c` shortcut. Keep it. Run `pnpm typecheck` — it names every over-deletion for you.

- [ ] **Step 7: Prove the behaviour did not change**

Run the whole frontend gate and the E2E suite:

```bash
pnpm lint && pnpm typecheck && pnpm test
pnpm test:e2e
```

Expected: green, including `tests/e2e/dashboard.spec.ts` and `tests/e2e/workflows.spec.ts`, both of which assert the landing heading is `Dashboard` and neither of which is touched by this task. A failure here is the whole reason this task lands alone.

- [ ] **Step 8: Commit**

```bash
git add src/App.vue src/features/agents/index.ts src/features/cockpit
git commit -m "refactor(dashboard): give the agent roster its own component"
```

---

### Task 2: The GitHub application's identity and capability catalogue

**Files:**
- Create: `server/internal/apps/github/app.go`
- Create: `server/internal/apps/github/app_test.go`
- Modify: `server/internal/settings/registry.go`

**Interfaces:**
- Consumes: `repo.ResourceRepo.Upsert`, `repo.CapabilityRepo.Upsert`, `repo.UpsertResourceInput`, `repo.UpsertCapabilityInput`, `repo.CapClassReach`, `repo.CapClassSpend`, `capability.EnforcerServer`.
- Produces:
  - `github.Slug = "github"`
  - `github.CapabilityRead = "github.read"`, `CapabilitySearch = "github.search"`, `CapabilityComment = "github.comment"`, `CapabilityMerge = "github.merge"`
  - `github.Capabilities() []CapabilityDecl` where `type CapabilityDecl struct { Name, Class string; Reversible bool }`
  - `github.Register(ctx context.Context, resources repo.ResourceRepo, caps repo.CapabilityRepo) error`
  - Settings keys `github.token`, `github.repos`, `github.baseURL`

`Capabilities()` is exported — Obsidian keeps its `capabilityDecls` private — because Task 6's two-surface parity test iterates it to assert every capability has both an HTTP route and an MCP tool. That test is the point of the whole slice; a private slice would force the parity list to be retyped, which is the "two places, one value" failure this project has hit repeatedly.

- [ ] **Step 1: Write the failing test**

Create `server/internal/apps/github/app_test.go`:

```go
package github_test

import (
	"context"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/apps/github"
	"github.com/lx-wnk/agent-dashboard/server/internal/capability"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

func newRepos(t *testing.T) (repo.ResourceRepo, repo.CapabilityRepo, context.Context) {
	t.Helper()
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = bundle.Client.Close() })
	return repo.NewResourceRepo(bundle.Client), repo.NewCapabilityRepo(bundle.Client), context.Background()
}

// TestRegisterPutsMergeInSpend is the decision this application turns on:
// capability.defaultEffect (decide.go:233-242) sends "spend" to deny and
// "reach" to ask, so the class alone is what makes an ungranted merge
// impossible rather than a prompt somebody can click through.
func TestRegisterPutsMergeInSpend(t *testing.T) {
	resources, caps, ctx := newRepos(t)
	if err := github.Register(ctx, resources, caps); err != nil {
		t.Fatalf("Register: %v", err)
	}

	want := map[string]struct {
		class      string
		reversible bool
	}{
		github.CapabilityRead:    {repo.CapClassReach, true},
		github.CapabilitySearch:  {repo.CapClassReach, true},
		github.CapabilityComment: {repo.CapClassReach, false},
		github.CapabilityMerge:   {repo.CapClassSpend, false},
	}
	for name, exp := range want {
		row, err := caps.Get(ctx, name)
		if err != nil {
			t.Fatalf("get %s: %v", name, err)
		}
		if row.Class != exp.class {
			t.Errorf("%s: Class = %q, want %q", name, row.Class, exp.class)
		}
		if row.Reversible != exp.reversible {
			t.Errorf("%s: Reversible = %v, want %v", name, row.Reversible, exp.reversible)
		}
		if len(row.EnforceableBy) != 1 || row.EnforceableBy[0] != capability.EnforcerServer {
			t.Errorf("%s: EnforceableBy = %v, want [%s]", name, row.EnforceableBy, capability.EnforcerServer)
		}
	}
}

func TestRegisterIsIdempotent(t *testing.T) {
	resources, caps, ctx := newRepos(t)
	if err := github.Register(ctx, resources, caps); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := github.Register(ctx, resources, caps); err != nil {
		t.Fatalf("second Register: %v", err)
	}

	apps, err := resources.ListForKind(ctx, repo.ResourceKindApplication)
	if err != nil {
		t.Fatalf("ListForKind: %v", err)
	}
	count := 0
	for _, a := range apps {
		if a.Slug == github.Slug {
			count++
		}
	}
	if count != 1 {
		t.Errorf("registry holds %d rows for slug %q, want exactly 1", count, github.Slug)
	}
}

// TestCapabilitiesMatchWhatRegisterWrote keeps the exported declaration list —
// which Task 6's surface-parity test iterates — from drifting away from the
// rows Register actually writes.
func TestCapabilitiesMatchWhatRegisterWrote(t *testing.T) {
	resources, caps, ctx := newRepos(t)
	if err := github.Register(ctx, resources, caps); err != nil {
		t.Fatalf("Register: %v", err)
	}
	decls := github.Capabilities()
	if len(decls) != 4 {
		t.Fatalf("Capabilities() returned %d decls, want 4", len(decls))
	}
	for _, d := range decls {
		row, err := caps.Get(ctx, d.Name)
		if err != nil {
			t.Fatalf("get %s: %v", d.Name, err)
		}
		if row.Class != d.Class || row.Reversible != d.Reversible {
			t.Errorf("%s: row = (%s, %v), decl = (%s, %v)", d.Name, row.Class, row.Reversible, d.Class, d.Reversible)
		}
	}
}
```

- [ ] **Step 2: Run the test and watch it fail**

Run: `cd server && go test ./internal/apps/github/`
Expected: `no Go files in .../apps/github` — the package does not exist yet.

- [ ] **Step 3: Write the application**

Create `server/internal/apps/github/app.go`:

```go
// Package github is the GitHub Application. Like apps/obsidian it is an
// in-server module rather than a subprocess plugin or a client to a foreign
// MCP server: a plugin hop adds no isolation (plugins already run in this
// machine's trust domain) and a foreign MCP server would ask the capability
// gate to rule on tool names this project does not define. The registry entry
// Register writes is what makes this an Application rather than ordinary
// server code.
package github

import (
	"context"
	"fmt"

	"github.com/lx-wnk/agent-dashboard/server/internal/capability"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// Slug identifies the GitHub application resource in the registry.
const Slug = "github"

// Capability names, per spec §4.2 (D2).
const (
	CapabilityRead    = "github.read"
	CapabilitySearch  = "github.search"
	CapabilityComment = "github.comment"
	CapabilityMerge   = "github.merge"
)

// CapabilityDecl is one row Register writes. Exported, unlike Obsidian's
// private equivalent, because the surface-parity test in
// internal/mcp/tools iterates it: it asserts that every capability declared
// here is reachable both over HTTP and as an MCP tool. A private list would
// force that test to retype the four names, which is exactly how a surface
// gets wired on one side only.
type CapabilityDecl struct {
	Name       string
	Class      string
	Reversible bool
}

// capabilityDecls declares the four GitHub capabilities.
//
// read, search and comment are class "reach": data leaves this machine
// toward a third party, or arrives from one — the same reasoning that puts
// the obsidian.* capabilities and WebFetch in "reach" rather than "tool".
//
// merge is class "spend", and that is the whole point of the class choice.
// capability.defaultEffect (internal/capability/decide.go:233-242) sends
// "spend" to EffectDeny and "reach" to EffectAsk, so with no grant a merge
// is refused outright rather than surfaced as a prompt a tired human clicks
// through at the end of a run. There is deliberately no "hold and prompt"
// fallback to rely on here.
//
// comment carries Reversible: false for the reason obsidian.write does: the
// comment is public the moment it posts, and deleting it afterwards does not
// unsend it. merge is irreversible for the obvious reason.
//
// Reversible is written to the catalogue and read by nothing today —
// capability.CapabilityView (decide.go:60-64) carries only Name, Class and
// EnforceableBy, and Decide never consults reversibility. It is recorded
// here anyway, same as apps/obsidian does, so the fact is stored where a
// future "a preset alone may not satisfy an irreversible capability" rule
// will look for it.
var capabilityDecls = []CapabilityDecl{
	{CapabilityRead, repo.CapClassReach, true},
	{CapabilitySearch, repo.CapClassReach, true},
	{CapabilityComment, repo.CapClassReach, false},
	{CapabilityMerge, repo.CapClassSpend, false},
}

// Capabilities returns a copy of the declarations, so a caller iterating them
// cannot reorder or mutate the catalogue this package is authoritative over.
func Capabilities() []CapabilityDecl {
	out := make([]CapabilityDecl, len(capabilityDecls))
	copy(out, capabilityDecls)
	return out
}

// Register gives GitHub its registry identity and catalogues its four
// capabilities. Idempotent — both Upsert calls resolve on conflict — so it is
// safe to run on every boot.
//
// Origin is Builtin, not Local: this ships in the server binary rather than
// being discovered on disk like a third-party plugin, so ResourceRepo refuses
// to let it be deleted. Scope is global: a personal access token is a
// machine-wide credential, not a per-project one.
func Register(ctx context.Context, resources repo.ResourceRepo, caps repo.CapabilityRepo) error {
	if _, err := resources.Upsert(ctx, repo.UpsertResourceInput{
		Kind:   repo.ResourceKindApplication,
		Slug:   Slug,
		Name:   "GitHub",
		Origin: repo.ResourceOriginBuiltin,
		Scope:  repo.GlobalScope(),
	}); err != nil {
		return fmt.Errorf("github.Register: %w", err)
	}

	for _, decl := range capabilityDecls {
		if _, err := caps.Upsert(ctx, repo.UpsertCapabilityInput{
			Name:          decl.Name,
			Class:         decl.Class,
			EnforceableBy: []string{capability.EnforcerServer},
			Reversible:    decl.Reversible,
		}); err != nil {
			return fmt.Errorf("github.Register: %w", err)
		}
	}
	return nil
}
```

- [ ] **Step 4: Add the three settings definitions**

In `server/internal/settings/registry.go`, directly after the four `obsidian.*` definitions (lines 135-138), add:

```go
		// github.token is the fine-grained PAT the GitHub Application
		// authenticates with. Secret, so it is encrypted at rest and masked on
		// every read except Service.Secret — and therefore carries no Default,
		// which Definition's own doc comment forbids for a secret.
		//
		// github.token and github.repos are a required PAIR:
		// serverapp.buildGitHubClient refuses to boot when exactly one is set.
		// github.baseURL is deliberately NOT part of that pair — it has a
		// Default, so it is never unset and cannot be a missing half of
		// anything.
		{Key: "github.token", Type: TypeString, Secret: true, Apply: ApplyRestart, Category: "github"},
		{Key: "github.repos", Type: TypeString, Default: "", Apply: ApplyRestart, Category: "github"},
		{Key: "github.baseURL", Type: TypeString, Default: "https://api.github.com", Apply: ApplyRestart, Category: "github"},
```

- [ ] **Step 5: Run the tests and watch them pass**

```bash
cd server && go test -count=1 ./internal/apps/github/ ./internal/settings/
```

- [ ] **Step 6: Commit**

```bash
cd server && gofmt -l internal/apps/github internal/settings && go vet ./...
git add server/internal/apps/github server/internal/settings/registry.go
git commit -m "feat(github): register GitHub as an application with four capabilities"
```

---

### Task 3: The GitHub client — the allow-list and four API calls, enforcing nothing

**Files:**
- Create: `server/internal/apps/github/client.go`
- Create: `server/internal/apps/github/client_test.go`

**Interfaces:**
- Consumes: `validation.SafeDialContext` (`server/internal/validation/ssrf.go:52`).
- Produces:
  - `github.ParseRepos(raw string) ([]string, error)`
  - `github.Config{Token, BaseURL string; Repos []string}`
  - `github.NewClient(cfg Config) (*Client, error)`
  - `(*Client).Repos() []string`
  - `(*Client).AllowsRepo(name string) bool`
  - `github.PullRequest{Number int; Title, Author, URL string; Draft bool; UpdatedAt time.Time}`
  - `github.SearchHit{Repo, Title, URL string; Number int}`
  - `(*Client).OpenPullRequests(ctx context.Context, repoName string, limit int) ([]PullRequest, error)`
  - `(*Client).SearchIssues(ctx context.Context, query string) ([]SearchHit, error)`
  - `(*Client).Comment(ctx context.Context, repoName string, number int, body string) (string, error)`
  - `(*Client).MergePullRequest(ctx context.Context, repoName string, number int, method string) (string, error)`

**Two design points, both stated so they are not rediscovered:**

1. **The client enforces no capability.** Decision D-A3 of the Obsidian slice, restated by spec §4.1: `Client` takes no capability repos, and a caller reaching it directly bypasses the gate. That is a known property. What the client *does* enforce is the repo allow-list, because that is a property of the configured client and not a permission question — the same way `resolveVaultPath` refuses a path outside `VaultRoot` regardless of what the gate said.
2. **`validation.SafeDialContext` is the dialer.** It resolves DNS and refuses any address that is loopback, private, link-local, CGNAT, multicast or unspecified. `api.github.com` is public, so this costs nothing there. **Consequence, and it is a real limitation:** a GitHub Enterprise instance on a LAN address (`10.x`, `192.168.x`) cannot be reached today, and `NewClient` will not tell you why until the first request fails to dial. This is not widened here — loosening a global SSRF guard for one application is exactly the trade the Obsidian client refused, and it solved the same problem with a narrow per-client dial policy instead. The follow-up, if GHE-on-LAN is ever wanted, is that narrow policy, not a change to `IsBlockedIP`.

- [ ] **Step 1: Write the failing test**

Create `server/internal/apps/github/client_test.go`:

```go
package github_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/apps/github"
)

func TestParseReposAcceptsOwnerNamePairsAndRejectsEverythingElse(t *testing.T) {
	got, err := github.ParseRepos(" lx-wnk/agent-dashboard , golang/go ")
	require.NoError(t, err)
	require.Equal(t, []string{"lx-wnk/agent-dashboard", "golang/go"}, got)

	empty, err := github.ParseRepos("")
	require.NoError(t, err)
	require.Empty(t, empty)

	for _, bad := range []string{"agent-dashboard", "a/b/c", "/name", "owner/", "own er/name"} {
		_, err := github.ParseRepos(bad)
		require.Errorf(t, err, "ParseRepos(%q) must refuse a malformed entry", bad)
	}
}

// newFakeGitHub serves the four endpoints the client calls and records the
// last request it saw, so a test can prove both what was sent and that
// nothing was sent at all.
func newFakeGitHub(t *testing.T) (*httptest.Server, *http.Request, *bool) {
	t.Helper()
	var last http.Request
	called := false
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/lx-wnk/agent-dashboard/pulls", func(w http.ResponseWriter, r *http.Request) {
		called, last = true, *r
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"number": 42, "title": "Add the cockpit", "draft": false,
			"html_url":   "https://github.com/lx-wnk/agent-dashboard/pull/42",
			"updated_at": "2026-09-01T10:00:00Z",
			"user":       map[string]any{"login": "lx-wnk"},
		}})
	})
	mux.HandleFunc("/search/issues", func(w http.ResponseWriter, r *http.Request) {
		called, last = true, *r
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{
			"number": 7, "title": "Flaky test",
			"html_url":      "https://github.com/lx-wnk/agent-dashboard/issues/7",
			"repository_url": "https://api.github.com/repos/lx-wnk/agent-dashboard",
		}}})
	})
	mux.HandleFunc("/repos/lx-wnk/agent-dashboard/issues/42/comments", func(w http.ResponseWriter, r *http.Request) {
		called, last = true, *r
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"html_url": "https://github.com/lx-wnk/agent-dashboard/pull/42#issuecomment-1"})
	})
	mux.HandleFunc("/repos/lx-wnk/agent-dashboard/pulls/42/merge", func(w http.ResponseWriter, r *http.Request) {
		called, last = true, *r
		_ = json.NewEncoder(w).Encode(map[string]any{"merged": true, "sha": "deadbeef"})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, &last, &called
}

func newTestClient(t *testing.T, ts *httptest.Server) *github.Client {
	t.Helper()
	c, err := github.NewClient(github.Config{
		Token:   "ghp_test",
		BaseURL: ts.URL,
		Repos:   []string{"lx-wnk/agent-dashboard"},
		// The fake server listens on loopback, which validation.SafeDialContext
		// refuses by design. Tests opt out of the guard; production never does.
		AllowLoopback: true,
	})
	require.NoError(t, err)
	return c
}

func TestOpenPullRequestsSendsTheTokenAndParsesTheAnswer(t *testing.T) {
	ts, last, _ := newFakeGitHub(t)
	prs, err := newTestClient(t, ts).OpenPullRequests(context.Background(), "lx-wnk/agent-dashboard", 5)
	require.NoError(t, err)
	require.Len(t, prs, 1)
	require.Equal(t, 42, prs[0].Number)
	require.Equal(t, "Add the cockpit", prs[0].Title)
	require.Equal(t, "lx-wnk", prs[0].Author)
	require.Equal(t, "Bearer ghp_test", last.Header.Get("Authorization"))
	require.Equal(t, "open", last.URL.Query().Get("state"))
	require.Equal(t, "5", last.URL.Query().Get("per_page"))
}

// TestEveryRepoScopedCallRefusesARepoOutsideTheAllowList is D4 at the client
// level: the allow-list is a property of the configured client, so it holds
// even for a caller that reached the client without asking the gate.
func TestEveryRepoScopedCallRefusesARepoOutsideTheAllowList(t *testing.T) {
	ts, _, called := newFakeGitHub(t)
	c := newTestClient(t, ts)
	ctx := context.Background()

	_, err := c.OpenPullRequests(ctx, "evil/repo", 5)
	require.ErrorIs(t, err, github.ErrRepoNotAllowed)
	_, err = c.Comment(ctx, "evil/repo", 1, "hi")
	require.ErrorIs(t, err, github.ErrRepoNotAllowed)
	_, err = c.MergePullRequest(ctx, "evil/repo", 1, "squash")
	require.ErrorIs(t, err, github.ErrRepoNotAllowed)

	require.False(t, *called, "no request may reach GitHub for a repository outside the allow-list")
	require.False(t, c.AllowsRepo("evil/repo"))
	require.True(t, c.AllowsRepo("lx-wnk/agent-dashboard"))
}

func TestCommentAndMergeReturnTheirResultURLs(t *testing.T) {
	ts, last, _ := newFakeGitHub(t)
	c := newTestClient(t, ts)
	ctx := context.Background()

	url, err := c.Comment(ctx, "lx-wnk/agent-dashboard", 42, "looks good")
	require.NoError(t, err)
	require.Contains(t, url, "issuecomment")
	require.Equal(t, http.MethodPost, last.Method)

	sha, err := c.MergePullRequest(ctx, "lx-wnk/agent-dashboard", 42, "squash")
	require.NoError(t, err)
	require.Equal(t, "deadbeef", sha)
	require.Equal(t, http.MethodPut, last.Method)
}

func TestSearchIssuesReportsTheOwningRepository(t *testing.T) {
	ts, last, _ := newFakeGitHub(t)
	hits, err := newTestClient(t, ts).SearchIssues(context.Background(), "is:open flaky")
	require.NoError(t, err)
	require.Len(t, hits, 1)
	require.Equal(t, "lx-wnk/agent-dashboard", hits[0].Repo)
	require.Equal(t, 7, hits[0].Number)
	require.Contains(t, last.URL.Query().Get("q"), "flaky")
}

// TestClientErrorsNeverCarryTheToken: an upstream 401 is the single most
// likely error a user will paste into an issue.
func TestClientErrorsNeverCarryTheToken(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	t.Cleanup(ts.Close)
	c, err := github.NewClient(github.Config{Token: "ghp_supersecret", BaseURL: ts.URL, Repos: []string{"lx-wnk/agent-dashboard"}, AllowLoopback: true})
	require.NoError(t, err)

	_, err = c.OpenPullRequests(context.Background(), "lx-wnk/agent-dashboard", 5)
	require.Error(t, err)
	require.False(t, strings.Contains(err.Error(), "ghp_supersecret"), "the token must never appear in an error: %v", err)
}

func TestNewClientRefusesAnUnparseableBaseURL(t *testing.T) {
	_, err := github.NewClient(github.Config{Token: "t", BaseURL: "://nope", Repos: []string{"a/b"}})
	require.Error(t, err)
}
```

- [ ] **Step 2: Run the test and watch it fail**

Run: `cd server && go test ./internal/apps/github/ -run 'TestParseRepos|TestOpenPullRequests|TestEveryRepo|TestComment|TestSearch|TestClientErrors|TestNewClient'`
Expected: compile failure — `undefined: github.ParseRepos`, `undefined: github.Config`, and the rest.

- [ ] **Step 3: Write the client**

Create `server/internal/apps/github/client.go`:

```go
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/validation"
)

// ErrRepoNotAllowed is returned for any repository outside github.repos. It is
// a sentinel so the HTTP handler and the MCP tools can answer 403/refuse
// without string-matching, and so a test can prove the refusal happened before
// any request was made.
var ErrRepoNotAllowed = errors.New("github: repository is not in the configured allow-list")

// repoRe is the shape of one github.repos entry. Deliberately stricter than
// GitHub's own rules: it exists to make the allow-list unambiguous, so a value
// that could be read two ways is refused rather than normalised.
const repoSegment = `[A-Za-z0-9._-]+`

// ParseRepos splits a comma-separated github.repos setting into owner/name
// pairs, refusing anything that is not exactly one owner and one name. An
// empty string parses to an empty list, which is how the application is
// switched off.
func ParseRepos(raw string) ([]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	var out []string
	for _, part := range strings.Split(trimmed, ",") {
		entry := strings.TrimSpace(part)
		if entry == "" {
			continue
		}
		owner, name, ok := strings.Cut(entry, "/")
		if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
			return nil, fmt.Errorf("github.repos: %q is not an owner/name pair", entry)
		}
		if !validSegment(owner) || !validSegment(name) {
			return nil, fmt.Errorf("github.repos: %q contains characters that are not valid in a repository path", entry)
		}
		out = append(out, entry)
	}
	return out, nil
}

func validSegment(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.', r == '_', r == '-':
		default:
			return false
		}
	}
	return s != ""
}

// Config configures a GitHub Client. Token and Repos are required; BaseURL
// defaults to the public API when empty.
type Config struct {
	Token   string
	BaseURL string
	Repos   []string
	// AllowLoopback disables validation.SafeDialContext for this client. It
	// exists for httptest servers, which listen on loopback — the address the
	// guard refuses by design. Production wiring (serverapp.buildGitHubClient)
	// never sets it.
	AllowLoopback bool
}

// Client talks to one GitHub API host on behalf of the configured token.
//
// It enforces the repository allow-list and nothing else. Capability checks
// belong to the callers (the HTTP handler and the MCP tools), which run them
// BEFORE calling in: a caller reaching this client directly bypasses the
// capability gate, exactly as apps/obsidian's Client does, and that is a
// stated property rather than an oversight.
type Client struct {
	http    *http.Client
	baseURL *url.URL
	token   string
	repos   map[string]bool
	order   []string
}

// NewClient validates cfg and builds a Client.
//
// The transport dials through validation.SafeDialContext, which re-resolves
// the host at connection time and refuses loopback, private, link-local and
// CGNAT addresses — the DNS-rebinding guard api/remotes already uses. The
// consequence is that a GitHub Enterprise host on a LAN address cannot be
// reached; widening the shared guard for one application is not the fix, a
// narrow per-client dial policy would be (cf. apps/obsidian's, which exists
// for the mirror-image reason).
func NewClient(cfg Config) (*Client, error) {
	if cfg.Token == "" {
		return nil, errors.New("github: Token is required")
	}
	raw := cfg.BaseURL
	if raw == "" {
		raw = "https://api.github.com"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("github: BaseURL %q is not an absolute URL", cfg.BaseURL)
	}
	if len(cfg.Repos) == 0 {
		return nil, errors.New("github: at least one repository is required")
	}

	transport := &http.Transport{}
	if !cfg.AllowLoopback {
		transport.DialContext = validation.SafeDialContext
	} else {
		transport.DialContext = (&net.Dialer{Timeout: 10 * time.Second}).DialContext
	}

	c := &Client{
		http:    &http.Client{Timeout: 20 * time.Second, Transport: transport},
		baseURL: u,
		token:   cfg.Token,
		repos:   make(map[string]bool, len(cfg.Repos)),
		order:   append([]string(nil), cfg.Repos...),
	}
	for _, r := range cfg.Repos {
		c.repos[r] = true
	}
	return c, nil
}

// Repos returns the configured allow-list in its configured order. A copy, so
// a caller cannot reorder the client's own list.
func (c *Client) Repos() []string {
	return append([]string(nil), c.order...)
}

// AllowsRepo reports whether name is in the allow-list. Callers use this to
// refuse a repository BEFORE consulting the capability gate (spec D4): a
// repository outside the list is refused without a capability question ever
// being asked, and the same owner/name string then goes to both the gate and
// the client.
func (c *Client) AllowsRepo(name string) bool { return c.repos[name] }

func (c *Client) checkRepo(name string) error {
	if !c.repos[name] {
		return fmt.Errorf("%w: %s", ErrRepoNotAllowed, name)
	}
	return nil
}

// do issues one authenticated request and decodes a JSON answer into out.
//
// The error it builds carries the status and the API's own message, never the
// request headers: a 401 is the error a user is most likely to paste
// somewhere public, and the Authorization header is on that request.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	u := *c.baseURL
	u.Path = strings.TrimSuffix(u.Path, "/") + path
	if query != nil {
		u.RawQuery = query.Encode()
	}

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("github: encode request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return fmt.Errorf("github: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// err from http.Client wraps *url.Error, whose Error() prints the URL
		// but never a header, so the token cannot ride along here.
		return fmt.Errorf("github: %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var apiErr struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 8<<10)).Decode(&apiErr)
		if apiErr.Message == "" {
			apiErr.Message = resp.Status
		}
		return fmt.Errorf("github: %s %s: %d %s", method, path, resp.StatusCode, apiErr.Message)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out); err != nil {
		return fmt.Errorf("github: decode %s %s: %w", method, path, err)
	}
	return nil
}

// PullRequest is one open pull request as the cockpit panel shows it.
type PullRequest struct {
	Number    int
	Title     string
	Author    string
	URL       string
	Draft     bool
	UpdatedAt time.Time
}

// OpenPullRequests lists the most recently updated open pull requests in
// repoName, newest first.
//
// One call per repository, rather than a single /search/issues query across
// all of them: search has its own much lower rate limit and its own eventual
// consistency, and the summary is the panel a user refreshes most.
func (c *Client) OpenPullRequests(ctx context.Context, repoName string, limit int) ([]PullRequest, error) {
	if err := c.checkRepo(repoName); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 5
	}
	var raw []struct {
		Number    int       `json:"number"`
		Title     string    `json:"title"`
		HTMLURL   string    `json:"html_url"`
		Draft     bool      `json:"draft"`
		UpdatedAt time.Time `json:"updated_at"`
		User      struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	q := url.Values{
		"state":     {"open"},
		"sort":      {"updated"},
		"direction": {"desc"},
		"per_page":  {strconv.Itoa(limit)},
	}
	if err := c.do(ctx, http.MethodGet, "/repos/"+repoName+"/pulls", q, nil, &raw); err != nil {
		return nil, err
	}
	out := make([]PullRequest, 0, len(raw))
	for _, r := range raw {
		out = append(out, PullRequest{
			Number: r.Number, Title: r.Title, Author: r.User.Login,
			URL: r.HTMLURL, Draft: r.Draft, UpdatedAt: r.UpdatedAt,
		})
	}
	return out, nil
}

// SearchHit is one issue or pull request matched by a search.
type SearchHit struct {
	Repo   string
	Number int
	Title  string
	URL    string
}

// SearchIssues runs one GitHub issue search.
//
// Unlike every other call here it names no single repository, so there is no
// allow-list check to make against a target — the query itself decides what it
// reaches. The callers narrow it instead: the HTTP route and the MCP tool both
// append a repo: qualifier per configured repository before calling in, so a
// search can never report a repository the operator did not list.
func (c *Client) SearchIssues(ctx context.Context, query string) ([]SearchHit, error) {
	var raw struct {
		Items []struct {
			Number        int    `json:"number"`
			Title         string `json:"title"`
			HTMLURL       string `json:"html_url"`
			RepositoryURL string `json:"repository_url"`
		} `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, "/search/issues", url.Values{"q": {query}, "per_page": {"20"}}, nil, &raw); err != nil {
		return nil, err
	}
	out := make([]SearchHit, 0, len(raw.Items))
	for _, item := range raw.Items {
		out = append(out, SearchHit{
			Repo:   repoFromAPIURL(item.RepositoryURL),
			Number: item.Number,
			Title:  item.Title,
			URL:    item.HTMLURL,
		})
	}
	return out, nil
}

// repoFromAPIURL turns "https://api.github.com/repos/owner/name" into
// "owner/name". The search API reports the owning repository only as this
// URL, and owner/name is the string every other surface here speaks.
func repoFromAPIURL(raw string) string {
	_, after, ok := strings.Cut(raw, "/repos/")
	if !ok {
		return ""
	}
	return strings.Trim(after, "/")
}

// Comment posts one comment on an issue or pull request and returns its URL.
// GitHub's issue-comment endpoint serves pull requests too — a pull request is
// an issue with a branch — so there is one method here, not two.
func (c *Client) Comment(ctx context.Context, repoName string, number int, body string) (string, error) {
	if err := c.checkRepo(repoName); err != nil {
		return "", err
	}
	if strings.TrimSpace(body) == "" {
		return "", errors.New("github: comment body must not be empty")
	}
	var out struct {
		HTMLURL string `json:"html_url"`
	}
	path := "/repos/" + repoName + "/issues/" + strconv.Itoa(number) + "/comments"
	if err := c.do(ctx, http.MethodPost, path, nil, map[string]string{"body": body}, &out); err != nil {
		return "", err
	}
	return out.HTMLURL, nil
}

// MergeMethods are the merge strategies GitHub accepts.
var MergeMethods = []string{"merge", "squash", "rebase"}

// MergePullRequest merges a pull request and returns the resulting commit SHA.
func (c *Client) MergePullRequest(ctx context.Context, repoName string, number int, method string) (string, error) {
	if err := c.checkRepo(repoName); err != nil {
		return "", err
	}
	if method == "" {
		method = "squash"
	}
	valid := false
	for _, m := range MergeMethods {
		if m == method {
			valid = true
			break
		}
	}
	if !valid {
		return "", fmt.Errorf("github: unknown merge method %q (valid: %s)", method, strings.Join(MergeMethods, ", "))
	}
	var out struct {
		Merged bool   `json:"merged"`
		SHA    string `json:"sha"`
	}
	path := "/repos/" + repoName + "/pulls/" + strconv.Itoa(number) + "/merge"
	if err := c.do(ctx, http.MethodPut, path, nil, map[string]string{"merge_method": method}, &out); err != nil {
		return "", err
	}
	if !out.Merged {
		return "", fmt.Errorf("github: %s#%d was not merged", repoName, number)
	}
	return out.SHA, nil
}
```

Delete the unused `repoSegment` constant if `gofmt`/`go vet` flags it — `validSegment` replaced it during writing; keep whichever the compiler accepts and do not keep both.

- [ ] **Step 4: Run the tests and watch them pass**

```bash
cd server && go test -count=1 ./internal/apps/github/
```

- [ ] **Step 5: Commit**

```bash
cd server && gofmt -l internal/apps/github && go vet ./...
git add server/internal/apps/github
git commit -m "feat(github): add a REST client bounded by a repository allow-list"
```

---

### Task 4: Boot wiring — the pair rule, and the failure that names the missing key

**Files:**
- Create: `server/serverapp/di_github.go`
- Create: `server/serverapp/di_github_test.go`
- Modify: `server/serverapp/di.go`

**Interfaces:**
- Consumes: `settings.Service.String`, `settings.Service.Secret`, `github.ParseRepos`, `github.NewClient`, `github.Register`.
- Produces: `buildGitHubClient(ctx context.Context, settingsSvc *settings.Service) (*github.Client, error)`, and a `githubClient *github.Client` local in `New` that later tasks read.

- [ ] **Step 1: Write the failing test**

Create `server/serverapp/di_github_test.go`. **Reuse the helper that already exists** — `newSettingsServiceForTest(t)` in `server/serverapp/di_obsidian_test.go:17-27` is in this same package, opens an in-memory database with a deterministic `secretbox.Box` (without which `Service.Secret` returns `ErrNoSecretBox`) and calls `svc.Load`. Do not write a second one.

```go
package serverapp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildGitHubClient_UnconfiguredIsNotAnError: an absent GitHub
// configuration must leave the rest of the server running, exactly as an
// absent Obsidian vault does.
func TestBuildGitHubClient_UnconfiguredIsNotAnError(t *testing.T) {
	svc := newSettingsServiceForTest(t) // nothing set
	client, err := buildGitHubClient(t.Context(), svc)
	require.NoError(t, err)
	assert.Nil(t, client, "an unconfigured GitHub must disable the feature, not fail the boot")
}

// github.token and github.repos are a required PAIR. Each direction gets its
// own test so a regression in one check cannot hide behind the other passing —
// the same shape TestBuildObsidianClient_Missing* uses for its trio.

func TestBuildGitHubClient_TokenWithoutReposIsAnError(t *testing.T) {
	svc := newSettingsServiceForTest(t)
	require.NoError(t, svc.Set(t.Context(), "github.token", "ghp_x"))
	_, err := buildGitHubClient(t.Context(), svc)
	require.Error(t, err, "a token with no repositories must fail loudly, not reach every repository the token can see")
	assert.Contains(t, err.Error(), "github.repos")
}

func TestBuildGitHubClient_ReposWithoutTokenIsAnError(t *testing.T) {
	svc := newSettingsServiceForTest(t)
	require.NoError(t, svc.Set(t.Context(), "github.repos", "lx-wnk/agent-dashboard"))
	_, err := buildGitHubClient(t.Context(), svc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "github.token")
}

// TestBuildGitHubClient_BaseURLIsNotHalfOfThePair pins the one place this
// differs from Obsidian's trio: github.baseURL carries a registry default, so
// it is never unset and can never be a missing half.
func TestBuildGitHubClient_BaseURLIsNotHalfOfThePair(t *testing.T) {
	svc := newSettingsServiceForTest(t)
	require.NoError(t, svc.Set(t.Context(), "github.baseURL", "https://api.github.com"))
	client, err := buildGitHubClient(t.Context(), svc)
	require.NoError(t, err, "a base URL alone must not fail the boot — it always has a value")
	assert.Nil(t, client)
}

// TestBuildGitHubClient_ErrorsNeverCarryTheToken: this function reads the
// decrypted secret, so it is the one place a boot error could leak it.
func TestBuildGitHubClient_ErrorsNeverCarryTheToken(t *testing.T) {
	svc := newSettingsServiceForTest(t)
	require.NoError(t, svc.Set(t.Context(), "github.token", "ghp_supersecret"))
	require.NoError(t, svc.Set(t.Context(), "github.repos", "not-an-owner-name-pair"))
	_, err := buildGitHubClient(t.Context(), svc)
	require.Error(t, err, "a malformed github.repos must fail the boot")
	assert.False(t, strings.Contains(err.Error(), "ghp_supersecret"), "boot error carries the token: %v", err)
}

func TestBuildGitHubClient_FullyConfiguredBuildsAClient(t *testing.T) {
	svc := newSettingsServiceForTest(t)
	require.NoError(t, svc.Set(t.Context(), "github.token", "ghp_x"))
	require.NoError(t, svc.Set(t.Context(), "github.repos", "lx-wnk/agent-dashboard, golang/go"))
	client, err := buildGitHubClient(t.Context(), svc)
	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, []string{"lx-wnk/agent-dashboard", "golang/go"}, client.Repos(),
		"the allow-list must keep its configured order — the summary panel lists repositories in it")
}
```

- [ ] **Step 2: Run the test and watch it fail**

Run: `cd server && go test ./serverapp/ -run TestBuildGitHubClient`
Expected: `undefined: buildGitHubClient`.

- [ ] **Step 3: Write the builder**

Create `server/serverapp/di_github.go`:

```go
package serverapp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/lx-wnk/agent-dashboard/server/internal/apps/github"
	"github.com/lx-wnk/agent-dashboard/server/internal/settings"
)

// buildGitHubClient returns nil, nil when GitHub is unconfigured: the
// application is optional, and an absent configuration must leave the rest of
// the server running.
//
// github.token and github.repos are a required PAIR — if one is set, both must
// be, because a client built from half of it would look configured and then
// fail every request instead of refusing to boot. This is the rule
// buildObsidianClient follows for its trio, with one difference forced by the
// settings registry: github.baseURL is NOT part of the pair. It carries a
// Default ("https://api.github.com"), so it is never unset and can never be a
// missing half of anything; a Secret definition may not carry a Default at all
// (settings.Definition's own doc comment), which is why the token has none.
//
// Nothing in this function may put the token in an error or a log line: it is
// the one place that holds the decrypted value.
func buildGitHubClient(ctx context.Context, settingsSvc *settings.Service) (*github.Client, error) {
	reposRaw := strings.TrimSpace(settingsSvc.String("github.repos"))
	token, err := settingsSvc.Secret(ctx, "github.token")
	if err != nil {
		return nil, fmt.Errorf("read github.token: %w", err)
	}
	token = strings.TrimSpace(token)

	if token == "" && reposRaw == "" {
		slog.Info("github: not configured, integration disabled")
		return nil, nil
	}

	var missing []string
	if token == "" {
		missing = append(missing, "github.token")
	}
	if reposRaw == "" {
		missing = append(missing, "github.repos")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required settings: %s", strings.Join(missing, ", "))
	}

	repos, err := github.ParseRepos(reposRaw)
	if err != nil {
		return nil, err
	}

	client, err := github.NewClient(github.Config{
		Token:   token,
		BaseURL: settingsSvc.String("github.baseURL"),
		Repos:   repos,
	})
	if err != nil {
		return nil, err
	}
	slog.Info("github: integration enabled", "repositories", len(repos))
	return client, nil
}
```

- [ ] **Step 4: Wire `Register` and the client into `di.go`**

In `server/serverapp/di.go`:

Add the import next to the obsidian one (`:54`):

```go
	"github.com/lx-wnk/agent-dashboard/server/internal/apps/github"
```

Declare the client alongside `obsidianClient` (`:304-305`):

```go
	// githubClient is nil when GitHub is unconfigured (buildGitHubClient's own
	// doc comment covers why that is not an error) and stays nil without a
	// database, since Register and the capability catalogue it depends on need
	// entClient too.
	var githubClient *github.Client
```

Inside the `if entClient != nil` block, immediately after the `obsidian.Register` call (`:347-349`), add:

```go
		// GitHub is the second builtin Application, and the reason this slice
		// exists: it goes through the same Register, the same capability
		// catalogue and the same encrypted-settings path Obsidian does. If it
		// ever needs a kernel change Obsidian did not, that is the finding.
		if err := github.Register(ctx, resourceRepo, capabilityRepo); err != nil {
			return nil, fmt.Errorf("github: register application: %w", err)
		}
```

and after `buildObsidianClient` (`:366-369`):

```go
		githubClient, err = buildGitHubClient(ctx, settingsSvc)
		if err != nil {
			return nil, fmt.Errorf("github: build client: %w", err)
		}
```

- [ ] **Step 5: Run the tests and watch them pass**

```bash
cd server && go test -count=1 ./serverapp/ -run TestBuildGitHubClient && go build ./...
```

- [ ] **Step 6: Commit**

```bash
cd server && gofmt -l serverapp && go vet ./...
git add server/serverapp/di_github.go server/serverapp/di_github_test.go server/serverapp/di.go
git commit -m "feat(github): build the client at boot and refuse a half-configured pair"
```

---

### Task 5: The HTTP surface — four routes, each gated

**Files:**
- Create: `server/internal/api/github/handler.go`
- Create: `server/internal/api/github/handler_test.go`
- Modify: `server/internal/api/router.go`
- Modify: `server/internal/api/testdata/routes.golden`
- Modify: `server/serverapp/di.go`

**Interfaces:**
- Consumes: `githubapp.Client`, `memory.Gate.Authorize(ctx, capName, value string, scope repo.Scope, extra ...capability.Context) error`, `apierr.ErrorMiddleware`, `apierr.NewAppError`, `apierr.WriteJSON`.
- Produces:
  - `githubapi.NewHandler(client *githubapp.Client, gate memory.Gate) *Handler`
  - `(*Handler).Mount(r chi.Router)` registering `GET /api/github/summary`, `GET /api/github/search`, `POST /api/github/comment`, `POST /api/github/merge`
  - `RouterDeps.GitHubHandler *githubapi.Handler`

**The order of checks is the contract, and it is the same in every one of the four handlers:**

1. client nil → 503 (`GitHub is not configured`)
2. parse and validate the request
3. `client.AllowsRepo(repo)` → 403 **without** consulting the gate (spec D4)
4. `gate.Authorize(ctx, capability, repo, repo.GlobalScope())`, mapping both `capability.ErrDenied` and `capability.ErrAskRequired` to 403
5. the client call

The value passed to `Authorize` is the same `owner/name` string passed to the client — the lesson `obsidian_read` records about normalizing before the gate. `github.search` has no single repository, so its value is `""`, for the reason `obsidian_search` documents at `obsidian.go:104-117`: `""` is not a wildcard, so a pattern-narrowed grant leaves search denied, and `*` or an empty pattern authorizes it.

- [ ] **Step 1: Write the failing test**

Create `server/internal/api/github/handler_test.go`:

```go
package github_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	githubapi "github.com/lx-wnk/agent-dashboard/server/internal/api/github"
	githubapp "github.com/lx-wnk/agent-dashboard/server/internal/apps/github"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/memory"
)

const testRepo = "lx-wnk/agent-dashboard"

// newEnv wires a Handler against an in-memory database with the GitHub
// capabilities catalogued (github.Register), and a fake GitHub that records
// whether it was reached. The Gate carries no Asker, so an "ask" effect fails
// closed and a test can tell deny from ask by the error text alone.
func newEnv(t *testing.T) (http.Handler, repo.GrantRepo, *bool, context.Context) {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })

	caps := repo.NewCapabilityRepo(bundle.Client)
	resources := repo.NewResourceRepo(bundle.Client)
	grants := repo.NewGrantRepo(bundle.Client)
	ctx := context.Background()
	require.NoError(t, githubapp.Register(ctx, resources, caps))

	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls"):
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"number": 42, "title": "t", "html_url": "u", "draft": false,
				"updated_at": "2026-09-01T10:00:00Z", "user": map[string]any{"login": "lx-wnk"},
			}})
		case strings.HasSuffix(r.URL.Path, "/merge"):
			_ = json.NewEncoder(w).Encode(map[string]any{"merged": true, "sha": "deadbeef"})
		case strings.HasSuffix(r.URL.Path, "/comments"):
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"html_url": "c"})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		}
	}))
	t.Cleanup(upstream.Close)

	client, err := githubapp.NewClient(githubapp.Config{
		Token: "ghp_supersecret", BaseURL: upstream.URL,
		Repos: []string{testRepo}, AllowLoopback: true,
	})
	require.NoError(t, err)

	h := githubapi.NewHandler(client, memory.Gate{
		Capabilities: caps,
		Grants:       grants,
		GrantUsage:   repo.NewGrantUsageRepo(bundle.Client, bundle.WriteClient),
	})
	r := chi.NewRouter()
	h.Mount(r)
	return r, grants, &called, ctx
}

func allowGlobally(t *testing.T, grants repo.GrantRepo, ctx context.Context, capName string) {
	t.Helper()
	_, err := grants.Create(ctx, repo.CreateGrantInput{
		CapabilityName: capName,
		Context:        repo.GrantContextFor(repo.GrantContextGlobal, ""),
		Pattern:        "*",
		Mode:           repo.GrantModeAllow,
		GrantedBy:      "test",
	})
	require.NoError(t, err)
}

func do(t *testing.T, h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestMergeIsDeniedWithNoGrantAndTheReasonNamesTheClassDefault is spec §6 row 1
// and the reason github.merge is class "spend": with no grant, Decide's
// defaultEffect denies outright rather than asking.
func TestMergeIsDeniedWithNoGrantAndTheReasonNamesTheClassDefault(t *testing.T) {
	h, _, called, _ := newEnv(t)
	rec := do(t, h, http.MethodPost, "/api/github/merge", `{"repo":"`+testRepo+`","number":42}`)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "capability denied")
	require.False(t, *called, "GitHub must not be reached before the gate allows the merge")
}

// TestMergeIsAllowedWithAnExplicitGlobalGrant is spec §6 row 2.
func TestMergeIsAllowedWithAnExplicitGlobalGrant(t *testing.T) {
	h, grants, called, ctx := newEnv(t)
	allowGlobally(t, grants, ctx, githubapp.CapabilityMerge)
	rec := do(t, h, http.MethodPost, "/api/github/merge", `{"repo":"`+testRepo+`","number":42}`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "deadbeef")
	require.True(t, *called)
}

// TestRepoOutsideTheAllowListIsRefusedBeforeTheGate is spec §6 row 3 and
// decision D4: no capability question is asked at all. Proven by granting
// merge globally and still being refused.
func TestRepoOutsideTheAllowListIsRefusedBeforeTheGate(t *testing.T) {
	h, grants, called, ctx := newEnv(t)
	allowGlobally(t, grants, ctx, githubapp.CapabilityMerge)
	rec := do(t, h, http.MethodPost, "/api/github/merge", `{"repo":"evil/repo","number":1}`)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "allow-list")
	require.NotContains(t, rec.Body.String(), "capability denied")
	require.False(t, *called)
}

func TestSummaryAndSearchAndCommentEachGateOnTheirOwnCapability(t *testing.T) {
	cases := []struct {
		name     string
		method   string
		target   string
		body     string
		grant    string
		otherCap string
	}{
		{"summary", http.MethodGet, "/api/github/summary", "", githubapp.CapabilityRead, githubapp.CapabilityMerge},
		{"search", http.MethodGet, "/api/github/search?q=flaky", "", githubapp.CapabilitySearch, githubapp.CapabilityRead},
		{"comment", http.MethodPost, "/api/github/comment", `{"repo":"` + testRepo + `","number":42,"body":"hi"}`, githubapp.CapabilityComment, githubapp.CapabilityRead},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A grant on a DIFFERENT capability must not open this route.
			h, grants, _, ctx := newEnv(t)
			allowGlobally(t, grants, ctx, tc.otherCap)
			rec := do(t, h, tc.method, tc.target, tc.body)
			require.Equal(t, http.StatusForbidden, rec.Code, "the wrong grant must not open %s", tc.target)

			h2, grants2, _, ctx2 := newEnv(t)
			allowGlobally(t, grants2, ctx2, tc.grant)
			rec2 := do(t, h2, tc.method, tc.target, tc.body)
			require.Equal(t, http.StatusOK, rec2.Code, "body: %s", rec2.Body.String())
		})
	}
}

// TestNoResponseEverCarriesTheToken is spec §6 row 5, on the HTTP surface.
func TestNoResponseEverCarriesTheToken(t *testing.T) {
	h, grants, _, ctx := newEnv(t)
	for _, c := range githubapp.Capabilities() {
		allowGlobally(t, grants, ctx, c.Name)
	}
	for _, target := range []string{"/api/github/summary", "/api/github/search?q=x"} {
		rec := do(t, h, http.MethodGet, target, "")
		require.NotContains(t, rec.Body.String(), "ghp_supersecret", "%s leaked the token", target)
	}
}

func TestUnconfiguredAnswers503NotAnEmptyList(t *testing.T) {
	h := githubapi.NewHandler(nil, memory.Gate{})
	r := chi.NewRouter()
	h.Mount(r)
	rec := do(t, r, http.MethodGet, "/api/github/summary", "")
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}
```

- [ ] **Step 2: Run the test and watch it fail**

Run: `cd server && go test ./internal/api/github/`
Expected: `no Go files` — the package does not exist.

- [ ] **Step 3: Write the handler**

Create `server/internal/api/github/handler.go`:

```go
// Package github implements the HTTP surface over the GitHub Application:
// one route per capability, each gated by memory.Gate.
//
// Every capability the application declares is reachable both here and as an
// MCP tool, and both enforce. That is not a preference: a seam wired on one
// surface only is a hole, and this project has shipped one twice. The
// surface-parity test in internal/mcp/tools asserts the pairing holds.
package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/lx-wnk/agent-dashboard/server/internal/apierr"
	githubapp "github.com/lx-wnk/agent-dashboard/server/internal/apps/github"
	"github.com/lx-wnk/agent-dashboard/server/internal/capability"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/memory"
)

// Handler serves /api/github/*.
type Handler struct {
	client *githubapp.Client
	gate   memory.Gate
}

// NewHandler creates a Handler. client is nil when GitHub is unconfigured
// (see serverapp.buildGitHubClient); every route then answers 503 rather than
// the route not existing at all, mirroring api/obsidian.
func NewHandler(client *githubapp.Client, gate memory.Gate) *Handler {
	return &Handler{client: client, gate: gate}
}

// Mount registers the /api/github/* routes on r.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/api/github/summary", apierr.ErrorMiddleware(h.summary))
	r.Get("/api/github/search", apierr.ErrorMiddleware(h.search))
	r.Post("/api/github/comment", apierr.ErrorMiddleware(h.comment))
	r.Post("/api/github/merge", apierr.ErrorMiddleware(h.merge))
}

// githubScope is the context every Authorize call below runs against. A
// personal access token is one machine-wide credential — github.Register
// catalogues the application at repo.GlobalScope() — so there is no
// caller-supplied scope to parse, matching the Obsidian tools.
func githubScope() repo.Scope { return repo.GlobalScope() }

func (h *Handler) ready() error {
	if h.client == nil {
		return apierr.NewAppError(http.StatusServiceUnavailable, "github is not configured")
	}
	return nil
}

// allow runs the two checks in the order decision D4 fixes: the repository
// allow-list FIRST, without a capability question, then the gate on the very
// same owner/name string the client will act on.
//
// Both capability.ErrDenied and capability.ErrAskRequired mean "forbidden" to
// this route's caller, so both map to 403 rather than the 500 ErrorMiddleware
// would give an unrecognised error.
func (h *Handler) allow(r *http.Request, capName, repoName string) error {
	if repoName != "" && !h.client.AllowsRepo(repoName) {
		return apierr.NewAppError(http.StatusForbidden,
			fmt.Sprintf("%s is not in the configured github.repos allow-list", repoName))
	}
	if err := h.gate.Authorize(r.Context(), capName, repoName, githubScope()); err != nil {
		if errors.Is(err, capability.ErrDenied) || errors.Is(err, capability.ErrAskRequired) {
			return apierr.NewAppError(http.StatusForbidden, err.Error())
		}
		return err
	}
	return nil
}

// pullRequestView is the camelCase JSON shape of one open pull request.
// Hand-written rather than encoding the client's struct: the wire format is
// this package's contract, and a field added to the client later must not
// silently become public.
type pullRequestView struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Author    string    `json:"author"`
	URL       string    `json:"url"`
	Draft     bool      `json:"draft"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// repoSummary carries one repository's open pull requests, or the reason that
// one repository could not be read. A per-repository Error, rather than one
// failed request for the whole panel: with three repositories configured, one
// rate-limited repository must not blank the other two.
type repoSummary struct {
	Repo         string            `json:"repo"`
	PullRequests []pullRequestView `json:"pullRequests"`
	Error        string            `json:"error,omitempty"`
}

type summaryResponse struct {
	Repos []repoSummary `json:"repos"`
}

// summaryPRLimit is how many open pull requests each repository contributes.
// A cockpit panel is a glance, not a list view.
const summaryPRLimit = 5

// summary answers GET /api/github/summary: the cockpit panel's data in one
// request, per spec §4.2.
func (h *Handler) summary(w http.ResponseWriter, r *http.Request) error {
	if err := h.ready(); err != nil {
		return err
	}
	repos := h.client.Repos()

	// One capability check for the whole summary, against "" — the request
	// names no single repository. A grant narrowed by pattern to one
	// repository therefore does NOT open the summary; that is deliberate, and
	// the same rule obsidian_search documents: "" is not the wildcard, an
	// empty or "*" grant pattern is.
	if err := h.allow(r, githubapp.CapabilityRead, ""); err != nil {
		return err
	}

	out := summaryResponse{Repos: make([]repoSummary, 0, len(repos))}
	for _, name := range repos {
		prs, err := h.client.OpenPullRequests(r.Context(), name, summaryPRLimit)
		if err != nil {
			out.Repos = append(out.Repos, repoSummary{Repo: name, PullRequests: []pullRequestView{}, Error: err.Error()})
			continue
		}
		views := make([]pullRequestView, 0, len(prs))
		for _, p := range prs {
			views = append(views, pullRequestView{
				Number: p.Number, Title: p.Title, Author: p.Author,
				URL: p.URL, Draft: p.Draft, UpdatedAt: p.UpdatedAt,
			})
		}
		out.Repos = append(out.Repos, repoSummary{Repo: name, PullRequests: views})
	}
	apierr.WriteJSON(w, http.StatusOK, out)
	return nil
}

type searchHitView struct {
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
}

// search answers GET /api/github/search?q=.
func (h *Handler) search(w http.ResponseWriter, r *http.Request) error {
	if err := h.ready(); err != nil {
		return err
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		return apierr.NewAppError(http.StatusBadRequest, "q is required")
	}
	if err := h.allow(r, githubapp.CapabilitySearch, ""); err != nil {
		return err
	}
	hits, err := h.client.SearchIssues(r.Context(), narrowToAllowedRepos(query, h.client.Repos()))
	if err != nil {
		return apierr.NewAppError(http.StatusBadGateway, err.Error())
	}
	out := make([]searchHitView, 0, len(hits))
	for _, hit := range hits {
		out = append(out, searchHitView{Repo: hit.Repo, Number: hit.Number, Title: hit.Title, URL: hit.URL})
	}
	apierr.WriteJSON(w, http.StatusOK, out)
	return nil
}

// narrowToAllowedRepos appends a repo: qualifier per configured repository, so
// a search can never report a repository the operator did not list. GitHub ORs
// repeated repo: qualifiers, so this widens nothing — it bounds the query to
// the same set the allow-list bounds every other call to.
func narrowToAllowedRepos(query string, repos []string) string {
	var b strings.Builder
	b.WriteString(query)
	for _, name := range repos {
		b.WriteString(" repo:")
		b.WriteString(name)
	}
	return b.String()
}

type repoActionRequest struct {
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	Body   string `json:"body"`
	Method string `json:"method"`
}

func decodeAction(r *http.Request) (repoActionRequest, error) {
	var req repoActionRequest
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(&req); err != nil {
		return req, apierr.NewAppError(http.StatusBadRequest, "invalid JSON body")
	}
	req.Repo = strings.TrimSpace(req.Repo)
	if req.Repo == "" {
		return req, apierr.NewAppError(http.StatusBadRequest, "repo is required")
	}
	if req.Number <= 0 {
		return req, apierr.NewAppError(http.StatusBadRequest, "number must be a positive issue or pull-request number")
	}
	return req, nil
}

// comment answers POST /api/github/comment.
func (h *Handler) comment(w http.ResponseWriter, r *http.Request) error {
	if err := h.ready(); err != nil {
		return err
	}
	req, err := decodeAction(r)
	if err != nil {
		return err
	}
	if strings.TrimSpace(req.Body) == "" {
		return apierr.NewAppError(http.StatusBadRequest, "body is required")
	}
	if err := h.allow(r, githubapp.CapabilityComment, req.Repo); err != nil {
		return err
	}
	url, err := h.client.Comment(r.Context(), req.Repo, req.Number, req.Body)
	if err != nil {
		return apierr.NewAppError(http.StatusBadGateway, err.Error())
	}
	apierr.WriteJSON(w, http.StatusOK, map[string]string{"url": url})
	return nil
}

// merge answers POST /api/github/merge.
//
// Registered exactly like the other three. Its capability class does the work:
// github.merge is class "spend", so with no grant capability.Decide returns
// deny — not ask — and no human is ever prompted into a merge.
func (h *Handler) merge(w http.ResponseWriter, r *http.Request) error {
	if err := h.ready(); err != nil {
		return err
	}
	req, err := decodeAction(r)
	if err != nil {
		return err
	}
	if err := h.allow(r, githubapp.CapabilityMerge, req.Repo); err != nil {
		return err
	}
	sha, err := h.client.MergePullRequest(r.Context(), req.Repo, req.Number, req.Method)
	if err != nil {
		return apierr.NewAppError(http.StatusBadGateway, err.Error())
	}
	apierr.WriteJSON(w, http.StatusOK, map[string]string{"sha": sha})
	return nil
}
```

- [ ] **Step 4: Mount it and update the golden**

In `server/internal/api/router.go`, add the import beside `apiobsidian`:

```go
	apigithub "github.com/lx-wnk/agent-dashboard/server/internal/api/github"
```

add the field to `RouterDeps` beside `ObsidianHandler` (`:178`):

```go
	GitHubHandler          *apigithub.Handler
```

and mount it immediately after the `ObsidianHandler` block (`:437-439`):

```go
		// GitHub's four routes stay session-authenticated like every other
		// write path in this group: two of them reach a third party in the
		// user's name, and one of them merges.
		if deps.GitHubHandler != nil {
			deps.GitHubHandler.Mount(r)
		}
```

In `server/serverapp/di.go`, next to the `obsidianHandler` construction (`:687-694`):

```go
	// GitHub — /api/github/*. Built WITH askerArg, unlike obsidianHandler
	// just above: a browser request has a human on the other end of it, so an
	// "ask" decision may legitimately hold for their answer. github.merge
	// never reaches the asker at all — its "spend" class resolves to deny in
	// Decide, and ServerEnforcer returns ErrDenied before the ask branch.
	var githubHandler *apigithub.Handler
	if entClient != nil {
		githubHandler = apigithub.NewHandler(githubClient, memory.Gate{
			Capabilities: repo.NewCapabilityRepo(entClient),
			Grants:       repo.NewGrantRepo(entClient),
			GrantUsage:   grantUsageRepo,
			Asker:        askerArg,
		})
	}
```

and add `GitHubHandler: githubHandler,` to the `RouterDeps` literal beside `ObsidianHandler:` (`:912`), plus the `apigithub` import.

Regenerate the golden and inspect it:

```bash
cd server && go test -count=1 ./internal/api/ -run TestRouteGolden -update-golden
git diff server/internal/api/testdata/routes.golden
```

Expected: exactly four added lines — `GET /api/github/search`, `GET /api/github/summary`, `POST /api/github/comment`, `POST /api/github/merge`. Any other change means something else moved; investigate before committing.

- [ ] **Step 5: Run the tests and watch them pass**

```bash
cd server && go test -count=1 ./internal/api/github/ ./internal/api/ && go build ./...
```

- [ ] **Step 6: Commit**

```bash
cd server && gofmt -l internal/api/github internal/api serverapp && go vet ./...
git add server/internal/api/github server/internal/api/router.go server/internal/api/testdata/routes.golden server/serverapp/di.go
git commit -m "feat(github): serve the four gated GitHub routes"
```

---

### Task 6: The MCP surface — four tools, and the parity test that keeps both surfaces honest

**Files:**
- Create: `server/internal/mcp/tools/github.go`
- Create: `server/internal/mcp/tools/github_test.go`
- Modify: `server/internal/mcp/auth.go`
- Modify: `server/internal/mcp/tools/keys.go`
- Modify: `server/serverapp/di_mcp.go`

**Interfaces:**
- Consumes: `mcp.ToolRegistry`, `mcp.ToolDef`, `mcp.StringArg`, `mcp.OptionalString`, `mcp.OK`, `mcp.Fail`, `memory.Gate`, `githubapp.Client`, `githubapp.Capabilities()`.
- Produces:
  - `mcptools.GitHubDeps{Client *githubapp.Client; Gate memory.Gate}`
  - `mcptools.RegisterGitHubTools(registry mcp.ToolRegistry, d GitHubDeps)`
  - `mcp.ToolScopeMap` entries: `github_read`→`github:read`, `github_search`→`github:read`, `github_comment`→`github:write`, `github_merge`→`github:merge`
  - `mcp.scopeImplies` entries: `github:read`→`{}`, `github:write`→`{github:read}`, `github:merge`→`{github:read}`, and all three added to `keys:manage`
  - `validKeyScopes` entries for all three

**Scope shape, decided here because the spec is silent on MCP scopes:** three scopes, not two. `github:merge` is its own scope and is *not* implied by `github:write`, so a key that may comment cannot merge by accident. `github:merge` implies `github:read` (you cannot sensibly merge what you cannot read) but not `github:write`. This is a second, coarser net *above* the capability gate — the gate is what actually decides a merge — and it means a compromised comment-only key cannot even reach the merge tool.

- [ ] **Step 1: Write the failing test**

Create `server/internal/mcp/tools/github_test.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	githubapp "github.com/lx-wnk/agent-dashboard/server/internal/apps/github"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	mcp "github.com/lx-wnk/agent-dashboard/server/internal/mcp"
	"github.com/lx-wnk/agent-dashboard/server/internal/memory"
)

const ghTestRepo = "lx-wnk/agent-dashboard"

// TestEveryGitHubCapabilityIsOnBothSurfaces is the rule this project has
// broken twice: a gated action wired on one surface only. It reads the
// application's own capability declarations — not a retyped list — and
// asserts each has both an HTTP route in the golden and an MCP tool with a
// scope entry. Adding a fifth capability without both surfaces fails here.
func TestEveryGitHubCapabilityIsOnBothSurfaces(t *testing.T) {
	byCapability := map[string]struct {
		tool  string
		route string
	}{
		githubapp.CapabilityRead:    {"github_read", "GET /api/github/summary"},
		githubapp.CapabilitySearch:  {"github_search", "GET /api/github/search"},
		githubapp.CapabilityComment: {"github_comment", "POST /api/github/comment"},
		githubapp.CapabilityMerge:   {"github_merge", "POST /api/github/merge"},
	}

	golden, err := os.ReadFile(filepath.Join("..", "..", "api", "testdata", "routes.golden"))
	require.NoError(t, err)

	deps, _, _, _ := newGitHubDepsForTest(t)
	registry := mcp.ToolRegistry{}
	RegisterGitHubTools(registry, deps)

	for _, decl := range githubapp.Capabilities() {
		pair, ok := byCapability[decl.Name]
		require.Truef(t, ok, "capability %s has no surface pair — add both an MCP tool and an HTTP route, never one", decl.Name)

		require.NotNilf(t, registry[pair.tool], "%s: MCP tool %s is not registered", decl.Name, pair.tool)
		_, hasScope := mcp.ToolScopeMap[pair.tool]
		require.Truef(t, hasScope, "%s: tool %s has no ToolScopeMap entry — Register panics at construction without one", decl.Name, pair.tool)
		require.Containsf(t, string(golden), pair.route+"\n", "%s: HTTP route %q is missing from the route golden", decl.Name, pair.route)
	}
}

func TestGitHubScopesAreGrantableAndImplyCorrectly(t *testing.T) {
	require.True(t, validKeyScopes["github:read"])
	require.True(t, validKeyScopes["github:write"])
	require.True(t, validKeyScopes["github:merge"])

	require.True(t, mcp.ResolveScopes([]string{"github:write"})["github:read"], "github:write must imply github:read")
	require.False(t, mcp.ResolveScopes([]string{"github:write"})["github:merge"], "a key that may comment must NOT be able to merge")
	require.True(t, mcp.ResolveScopes([]string{"github:merge"})["github:read"], "github:merge must imply github:read")

	all := mcp.ResolveScopes([]string{"keys:manage"})
	for _, s := range []string{"github:read", "github:write", "github:merge"} {
		require.Truef(t, all[s], "keys:manage must imply %s — scopeImplies is one level deep, so it must list them by hand", s)
	}
}

func newGitHubDepsForTest(t *testing.T) (GitHubDeps, repo.GrantRepo, *bool, context.Context) {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })

	caps := repo.NewCapabilityRepo(bundle.Client)
	resources := repo.NewResourceRepo(bundle.Client)
	grants := repo.NewGrantRepo(bundle.Client)
	ctx := context.Background()
	require.NoError(t, githubapp.Register(ctx, resources, caps))

	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		switch {
		case strings.HasSuffix(r.URL.Path, "/merge"):
			_ = json.NewEncoder(w).Encode(map[string]any{"merged": true, "sha": "deadbeef"})
		case strings.HasSuffix(r.URL.Path, "/comments"):
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"html_url": "c"})
		case strings.HasSuffix(r.URL.Path, "/pulls"):
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		}
	}))
	t.Cleanup(upstream.Close)

	client, err := githubapp.NewClient(githubapp.Config{
		Token: "ghp_supersecret", BaseURL: upstream.URL,
		Repos: []string{ghTestRepo}, AllowLoopback: true,
	})
	require.NoError(t, err)

	// No Asker, so the ask effect fails closed and a test can tell a denied
	// merge from an ask-required comment by the error text.
	return GitHubDeps{Client: client, Gate: memory.Gate{
		Capabilities: caps, Grants: grants,
		GrantUsage: repo.NewGrantUsageRepo(bundle.Client, bundle.WriteClient),
	}}, grants, &called, ctx
}

func grantGitHub(t *testing.T, grants repo.GrantRepo, ctx context.Context, capName string) {
	t.Helper()
	_, err := grants.Create(ctx, repo.CreateGrantInput{
		CapabilityName: capName,
		Context:        repo.GrantContextFor(repo.GrantContextGlobal, ""),
		Pattern:        "*",
		Mode:           repo.GrantModeAllow,
		GrantedBy:      "test",
	})
	require.NoError(t, err)
}

// TestGitHubMergeDeniedBeforeAnyRequest: class "spend" denies with no grant,
// and the deny happens before GitHub is contacted.
func TestGitHubMergeDeniedBeforeAnyRequest(t *testing.T) {
	deps, _, called, ctx := newGitHubDepsForTest(t)
	registry := mcp.ToolRegistry{}
	RegisterGitHubTools(registry, deps)

	_, err := registry["github_merge"].Handler(ctx, map[string]any{"repo": ghTestRepo, "number": float64(42)})
	require.Error(t, err)
	require.ErrorContains(t, err, "capability denied")
	require.False(t, *called, "GitHub must not be reached before the gate allows the merge")
}

// TestGitHubMergeAllowedWithAnExplicitGrant: same grant, MCP surface — the
// mirror of the HTTP test in internal/api/github. Neither surface is open and
// neither is closed when the other is not.
func TestGitHubMergeAllowedWithAnExplicitGrant(t *testing.T) {
	deps, grants, called, ctx := newGitHubDepsForTest(t)
	grantGitHub(t, grants, ctx, githubapp.CapabilityMerge)
	registry := mcp.ToolRegistry{}
	RegisterGitHubTools(registry, deps)

	res, err := registry["github_merge"].Handler(ctx, map[string]any{"repo": ghTestRepo, "number": float64(42)})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.True(t, *called)
}

// TestGitHubToolsRefuseARepoOutsideTheAllowListBeforeTheGate is D4 on the MCP
// surface: merge is granted globally and the call is still refused, with a
// message about the allow-list rather than a capability.
func TestGitHubToolsRefuseARepoOutsideTheAllowListBeforeTheGate(t *testing.T) {
	deps, grants, called, ctx := newGitHubDepsForTest(t)
	grantGitHub(t, grants, ctx, githubapp.CapabilityMerge)
	registry := mcp.ToolRegistry{}
	RegisterGitHubTools(registry, deps)

	_, err := registry["github_merge"].Handler(ctx, map[string]any{"repo": "evil/repo", "number": float64(1)})
	require.Error(t, err)
	require.ErrorContains(t, err, "allow-list")
	require.NotContains(t, err.Error(), "capability denied")
	require.False(t, *called)
}

// TestGitHubToolErrorsNeverCarryTheToken is spec §6 row 5 on the MCP surface.
func TestGitHubToolErrorsNeverCarryTheToken(t *testing.T) {
	deps, _, _, ctx := newGitHubDepsForTest(t)
	registry := mcp.ToolRegistry{}
	RegisterGitHubTools(registry, deps)

	for _, name := range []string{"github_read", "github_search", "github_comment", "github_merge"} {
		_, err := registry[name].Handler(ctx, map[string]any{"repo": ghTestRepo, "number": float64(1), "body": "x", "query": "x"})
		if err != nil {
			require.NotContains(t, err.Error(), "ghp_supersecret", "%s leaked the token", name)
		}
	}
}

// TestNoGitHubToolIsRegisteredWhenUnconfigured mirrors
// RegisterObsidianTools: an agent discovering a tool it can never use is worse
// than not discovering it.
func TestNoGitHubToolIsRegisteredWhenUnconfigured(t *testing.T) {
	registry := mcp.ToolRegistry{}
	RegisterGitHubTools(registry, GitHubDeps{})
	for _, name := range []string{"github_read", "github_search", "github_comment", "github_merge"} {
		require.Nil(t, registry[name], "%s must not be registered when GitHub is unconfigured", name)
	}
}
```

Add `"os"` and `"path/filepath"` to that file's imports — the parity test reads the route golden from disk.

- [ ] **Step 2: Run the test and watch it fail**

Run: `cd server && go test ./internal/mcp/tools/ -run 'TestGitHub|TestEveryGitHub|TestNoGitHub'`
Expected: compile failure — `undefined: GitHubDeps`, `undefined: RegisterGitHubTools`.

- [ ] **Step 3: Write the tools**

Create `server/internal/mcp/tools/github.go`:

```go
package tools

import (
	"context"
	"fmt"

	githubapp "github.com/lx-wnk/agent-dashboard/server/internal/apps/github"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	mcp "github.com/lx-wnk/agent-dashboard/server/internal/mcp"
	"github.com/lx-wnk/agent-dashboard/server/internal/memory"
)

// GitHubDeps holds the dependencies required by the GitHub MCP tools. Client
// enforces no capability of its own (see its doc comment) — Gate is what makes
// every handler below safe to call.
//
// Gate is built WITH an Asker in production (serverapp/di_mcp.go), like the
// Obsidian tools: an MCP call has an agent genuinely waiting on the response,
// so an ask decision may hold for a human's answer. github_merge never reaches
// the asker regardless — its "spend" class resolves to deny in
// capability.Decide, and ServerEnforcer returns ErrDenied before the ask
// branch, so nobody can be prompted into a merge.
type GitHubDeps struct {
	Client *githubapp.Client
	Gate   memory.Gate
}

// githubScope: one machine-wide credential, so global scope, no caller-supplied
// scope to parse. Same reasoning as obsidianScope.
func githubScope() repo.Scope { return repo.GlobalScope() }

// authorize runs the allow-list check and the gate in the order decision D4
// fixes: the repository FIRST, without a capability question, then the gate on
// the very same owner/name string the client will act on. repoName is "" for
// calls that name no single repository.
//
// Gate.Authorize is called with four arguments, not with caller contexts:
// mcp.CallerResolver does not exist on this branch (it belongs to the
// stage-run-credentials work). memory.Gate.Authorize's variadic
// `extra ...capability.Context` already accepts them, so when that lands these
// four call sites take the same one-line edit as the Obsidian ones.
func (d GitHubDeps) authorize(ctx context.Context, capName, repoName string) error {
	if repoName != "" && !d.Client.AllowsRepo(repoName) {
		return mcp.Fail(fmt.Sprintf("%s is not in the configured github.repos allow-list", repoName))
	}
	if err := d.Gate.Authorize(ctx, capName, repoName, githubScope()); err != nil {
		return mcp.Fail(err.Error())
	}
	return nil
}

// RegisterGitHubTools registers the 4 GitHub MCP tools. When d.Client is nil —
// GitHub is unconfigured — no tool is registered at all, mirroring
// RegisterObsidianTools.
func RegisterGitHubTools(registry mcp.ToolRegistry, d GitHubDeps) {
	if d.Client == nil {
		return
	}
	registerGitHubRead(registry, d)
	registerGitHubSearch(registry, d)
	registerGitHubComment(registry, d)
	registerGitHubMerge(registry, d)
}

// numberArg reads a positive issue or pull-request number. JSON numbers decode
// as float64, so an int argument cannot be read with StringArg's shape.
func numberArg(args map[string]any, key string) (int, error) {
	raw, ok := args[key]
	if !ok {
		return 0, mcp.Fail(key + " is required")
	}
	f, ok := raw.(float64)
	if !ok {
		return 0, mcp.Fail(key + " must be a number")
	}
	n := int(f)
	if float64(n) != f || n <= 0 {
		return 0, mcp.Fail(key + " must be a positive whole number")
	}
	return n, nil
}

func registerGitHubRead(registry mcp.ToolRegistry, d GitHubDeps) {
	registry.Register(&mcp.ToolDef{
		Name:        "github_read",
		Description: "List the most recently updated open pull requests in one of the configured GitHub repositories.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"repo":  map[string]any{"type": "string", "description": "owner/name, and it must be listed in the github.repos setting"},
				"limit": map[string]any{"type": "number", "description": "How many pull requests to return (default 5)"},
			},
			"required": []string{"repo"},
		},
		Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
			repoName, err := mcp.StringArg(args, "repo")
			if err != nil {
				return nil, err
			}
			// The repository is the capability value, so a grant can be narrowed
			// to one repository by pattern instead of opening all of them.
			if err := d.authorize(ctx, githubapp.CapabilityRead, repoName); err != nil {
				return nil, err
			}
			limit := 5
			if n, ok := args["limit"].(float64); ok && n > 0 {
				limit = int(n)
			}
			prs, err := d.Client.OpenPullRequests(ctx, repoName, limit)
			if err != nil {
				return nil, mcp.Fail("github_read: " + err.Error())
			}
			return mcp.OK(map[string]any{"repo": repoName, "pullRequests": prs})
		},
	})
}

func registerGitHubSearch(registry mcp.ToolRegistry, d GitHubDeps) {
	registry.Register(&mcp.ToolDef{
		Name:        "github_search",
		Description: "Search issues and pull requests across the configured GitHub repositories.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "GitHub issue-search query"},
			},
			"required": []string{"query"},
		},
		Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
			query, err := mcp.StringArg(args, "query")
			if err != nil {
				return nil, err
			}
			// A search names no single repository, so there is no value to pass.
			// "" is NOT a wildcard here — see obsidian_search's comment for the
			// full reasoning: a grant narrowed to a literal prefix never matches
			// "", so search stays denied; an empty or "*" pattern authorizes it.
			if err := d.authorize(ctx, githubapp.CapabilitySearch, ""); err != nil {
				return nil, err
			}
			// Bound the query to the allow-list before it leaves, the same way
			// the HTTP route does — a search must never report a repository the
			// operator did not list.
			bounded := query
			for _, name := range d.Client.Repos() {
				bounded += " repo:" + name
			}
			hits, err := d.Client.SearchIssues(ctx, bounded)
			if err != nil {
				return nil, mcp.Fail("github_search: " + err.Error())
			}
			return mcp.OK(map[string]any{"results": hits})
		},
	})
}

func registerGitHubComment(registry mcp.ToolRegistry, d GitHubDeps) {
	registry.Register(&mcp.ToolDef{
		Name:        "github_comment",
		Description: "Post a comment on a GitHub issue or pull request. Irreversible: the comment is public the moment it posts.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"repo":   map[string]any{"type": "string", "description": "owner/name, and it must be listed in the github.repos setting"},
				"number": map[string]any{"type": "number", "description": "Issue or pull-request number"},
				"body":   map[string]any{"type": "string", "description": "Comment body (Markdown)"},
			},
			"required": []string{"repo", "number", "body"},
		},
		Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
			repoName, err := mcp.StringArg(args, "repo")
			if err != nil {
				return nil, err
			}
			number, err := numberArg(args, "number")
			if err != nil {
				return nil, err
			}
			body, err := mcp.StringArg(args, "body")
			if err != nil {
				return nil, err
			}
			if err := d.authorize(ctx, githubapp.CapabilityComment, repoName); err != nil {
				return nil, err
			}
			url, err := d.Client.Comment(ctx, repoName, number, body)
			if err != nil {
				return nil, mcp.Fail("github_comment: " + err.Error())
			}
			return mcp.OK(map[string]any{"url": url})
		},
	})
}

func registerGitHubMerge(registry mcp.ToolRegistry, d GitHubDeps) {
	registry.Register(&mcp.ToolDef{
		Name:        "github_merge",
		Description: "Merge a GitHub pull request. Irreversible, and denied unless a human created an explicit github.merge grant.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"repo":   map[string]any{"type": "string", "description": "owner/name, and it must be listed in the github.repos setting"},
				"number": map[string]any{"type": "number", "description": "Pull-request number"},
				"method": map[string]any{"type": "string", "description": "merge, squash or rebase (default squash)"},
			},
			"required": []string{"repo", "number"},
		},
		Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
			repoName, err := mcp.StringArg(args, "repo")
			if err != nil {
				return nil, err
			}
			number, err := numberArg(args, "number")
			if err != nil {
				return nil, err
			}
			// Registered like the other three; the class does the work. With no
			// grant, capability.Decide's defaultEffect sends "spend" to deny.
			if err := d.authorize(ctx, githubapp.CapabilityMerge, repoName); err != nil {
				return nil, err
			}
			sha, err := d.Client.MergePullRequest(ctx, repoName, number, mcp.OptionalString(args, "method"))
			if err != nil {
				return nil, mcp.Fail("github_merge: " + err.Error())
			}
			return mcp.OK(map[string]any{"repo": repoName, "number": number, "sha": sha})
		},
	})
}
```

- [ ] **Step 4: Add the scopes**

In `server/internal/mcp/auth.go`, add to `ToolScopeMap` after the obsidian block:

```go
	// github:read / github:write / github:merge
	"github_read": "github:read", "github_search": "github:read",
	"github_comment": "github:write",
	"github_merge":   "github:merge",
```

and to `scopeImplies`:

```go
	"github:read":  {},
	"github:write": {"github:read"},
	// merge implies read, deliberately NOT write: a key that may comment must
	// not be able to merge, and a key that may merge has no business editing
	// discussions. The capability gate is what actually decides a merge; this
	// is the coarser net above it.
	"github:merge": {"github:read"},
```

and extend the `keys:manage` list (scopeImplies is one level deep, so it must name them by hand):

```go
	"keys:manage": {
		"tasks:read", "tasks:write", "pipeline:control", "agent:coord",
		"memory:read", "memory:write", "obsidian:read", "obsidian:write",
		"github:read", "github:write", "github:merge",
	},
```

In `server/internal/mcp/tools/keys.go`, add to `validKeyScopes`:

```go
	"github:read":   true,
	"github:write":  true,
	"github:merge":  true,
```

and extend the `invalid scope` message at `keys.go:115` with `, github:read, github:write, github:merge`.

- [ ] **Step 5: Wire the tools**

In `server/serverapp/di_mcp.go`, add the parameter to `provideMCPHandler` after `obsidianClient`:

```go
	githubClient *github.Client,
```

(with `github "github.com/lx-wnk/agent-dashboard/server/internal/apps/github"` imported), register the tools next to `RegisterObsidianTools` (`:137`):

```go
	// Same Asker as the memory and Obsidian tools: an agent is waiting on the
	// tool response either way. RegisterGitHubTools itself skips registering
	// anything when githubClient is nil.
	mcptools.RegisterGitHubTools(registry, mcptools.GitHubDeps{
		Client: githubClient,
		Gate: memory.Gate{
			Capabilities: repo.NewCapabilityRepo(client),
			Grants:       repo.NewGrantRepo(client),
			GrantUsage:   grantUsageRepo,
			Asker:        memAsker,
		},
	})
```

Copy the exact `memory.Gate` literal the `RegisterObsidianTools` call above it uses — field names and sources must match, not be reinvented. Then pass `githubClient` at the one call site in `di.go:637`.

- [ ] **Step 6: Run the tests and watch them pass**

```bash
cd server && go test -count=1 ./internal/mcp/... && go build ./...
```

- [ ] **Step 7: Commit**

```bash
cd server && gofmt -l internal/mcp serverapp && go vet ./...
git add server/internal/mcp server/serverapp/di_mcp.go server/serverapp/di.go
git commit -m "feat(github): expose the four GitHub capabilities as MCP tools"
```

---

### Task 7: The cockpit shell and its four non-GitHub panels

**Files:**
- Create: `src/features/cockpit/panelState.ts`
- Create: `src/features/cockpit/components/CockpitPanel.vue`
- Create: `src/features/cockpit/components/CockpitPanel.test.ts`
- Create: `src/features/cockpit/components/CockpitView.vue`
- Create: `src/features/cockpit/components/AgentsPanel.vue`
- Create: `src/features/cockpit/components/PipelinePanel.vue`
- Create: `src/features/cockpit/components/RoutinesPanel.vue`
- Create: `src/features/cockpit/components/MemoryPanel.vue`
- Modify: `src/composables/useViewState.ts`
- Modify: `src/utils/navConfig.ts`, `src/utils/navConfig.test.ts`
- Modify: `src/App.vue`
- Modify: `tests/e2e/dashboard.spec.ts`, `tests/e2e/workflows.spec.ts`

**Interfaces:**
- Produces:
  - `export type PanelState = 'loading' | 'notAsked' | 'denied' | 'empty' | 'failed' | 'ready'`
  - `CockpitPanel.vue` props `{ id: string; title: string; state: PanelState; message?: string }`, default slot rendered only when `state === 'ready'`
  - `ActiveView` gains `'cockpit'`; `ACTIVE_VIEWS` gains `'cockpit'`; the fallback default becomes `'cockpit'`
- Consumes: `useResources()` from `@/features/settings` (see the barrel note below), `useAgents`, `useTasks`.

**The five-state rule, made testable rather than aspirational.** `CockpitPanel.vue` is the single owner of the five non-ready states, so no panel can collapse two of them by accident: the shell renders exactly one `data-testid="cockpit-<id>-<state>"` element, chosen by one `v-if`/`v-else-if` chain in one file, and renders the default slot only in `ready`. A panel supplies a `state` and nothing else. The panel's own test then asserts, per state, that its testid is present **and** every sibling testid has count 0 — the same shape `tests/e2e/settings-gated-panels.spec.ts` already uses.

**Where a state is structurally unreachable, say so rather than faking it.** The spec asks for five states per panel. Two panels cannot reach all five, and inventing an unreachable branch would be a lie the tests would have to pretend to check:

| Panel | Source | States it can reach | Why the others cannot occur |
|---|---|---|---|
| Agents | `useAgents` SSE singleton | loading, empty, failed, ready | No gate on the agent stream, so no `denied`; it streams from mount, so no `notAsked` |
| Pipeline | `useTasks` SSE singleton | loading, empty, failed, ready | Same |
| Routines | `GET /api/resources?kind=routine` | loading, empty, failed, ready | `kind=routine` is deliberately ungated (`api/resources/handler.go`, the `if kind == ResourceKindMemorySpace` comment) — it answers the same rows `/api/schedules` already serves the same caller |
| Memory | `GET /api/resources?kind=memory_space` | loading, denied, empty, failed, ready | All five bar `notAsked`: it fetches on mount |
| GitHub (Task 8) | `GET /api/github/summary` | all five | `notAsked` is real here: `github.repos` empty means the application is off and no request is made |

- [ ] **Step 1: Write the failing test**

Create `src/features/cockpit/components/CockpitPanel.test.ts`:

```ts
import type { PanelState } from '../panelState'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { PANEL_STATES } from '../panelState'
import CockpitPanel from './CockpitPanel.vue'

// The five-state rule, as an assertion rather than a promise: for every state
// exactly one state marker is in the DOM, and the content slot appears only in
// "ready". A panel that collapsed two states would fail here, once, for every
// panel that uses the shell.
describe('cockpitPanel', () => {
  const others = (state: PanelState) => PANEL_STATES.filter(s => s !== state)

  it.each(PANEL_STATES)('renders exactly the %s marker and nothing else', (state) => {
    const wrapper = mount(CockpitPanel, {
      props: { id: 'demo', title: 'Demo', state, message: 'because' },
      slots: { default: '<p data-testid="demo-content">rows</p>' },
    })

    if (state === 'ready') {
      expect(wrapper.find('[data-testid="demo-content"]').exists()).toBe(true)
    }
    else {
      expect(wrapper.find(`[data-testid="cockpit-demo-${state}"]`).exists()).toBe(true)
      expect(wrapper.find('[data-testid="demo-content"]').exists()).toBe(false)
    }

    for (const other of others(state))
      expect(wrapper.findAll(`[data-testid="cockpit-demo-${other}"]`)).toHaveLength(0)
  })

  it('shows the server message on denied and on failed, and never invents one', () => {
    const denied = mount(CockpitPanel, { props: { id: 'demo', title: 'Demo', state: 'denied', message: 'memory.read is not granted in this scope' } })
    expect(denied.get('[data-testid="cockpit-demo-denied"]').text()).toContain('memory.read is not granted in this scope')

    const failed = mount(CockpitPanel, { props: { id: 'demo', title: 'Demo', state: 'failed', message: 'HTTP 500' } })
    expect(failed.get('[data-testid="cockpit-demo-failed"]').text()).toContain('HTTP 500')
  })
})
```

- [ ] **Step 2: Run the test and watch it fail**

Run: `pnpm test src/features/cockpit/components/CockpitPanel.test.ts`
Expected: `Failed to resolve import "../panelState"`.

- [ ] **Step 3: Write the state type and the shell**

Create `src/features/cockpit/panelState.ts`:

```ts
/**
 * The states a cockpit panel can be in. Exactly one is rendered at a time, by
 * CockpitPanel.vue, which is their single owner — a panel that collapsed two
 * of them (an unanswered request drawn as an empty list, a refusal drawn as
 * "nothing here") is the defect this type exists to prevent.
 *
 * - loading  — a request is in flight
 * - notAsked — no request was made, and the panel knows why (nothing configured)
 * - denied   — the server refused (HTTP 403), and named a reason
 * - empty    — the server answered, and the answer was nothing
 * - failed   — the request errored, which is not the same as being refused
 * - ready    — the content slot renders
 */
export const PANEL_STATES = ['loading', 'notAsked', 'denied', 'empty', 'failed', 'ready'] as const
export type PanelState = typeof PANEL_STATES[number]
```

Create `src/features/cockpit/components/CockpitPanel.vue`:

```vue
<script setup lang="ts">
import type { PanelState } from '../panelState'

const props = defineProps<{
  id: string
  title: string
  state: PanelState
  /** The server's own words for denied and failed; the fallback line for the rest. */
  message?: string
}>()

// One place decides which state marker exists, so five panels cannot drift.
const testid = (state: PanelState) => `cockpit-${props.id}-${state}`
</script>

<template>
  <section
    class="bg-card border border-line rounded-xl p-4 flex flex-col gap-3 min-w-0"
    :data-testid="`cockpit-panel-${id}`"
    :aria-busy="state === 'loading'"
  >
    <header class="flex items-center justify-between gap-2">
      <h2 class="text-[13px] font-semibold text-fg">
        {{ title }}
      </h2>
      <slot name="action" />
    </header>

    <div v-if="state === 'loading'" :data-testid="testid('loading')" class="text-[12px] text-fg-mute" role="status">
      Loading…
    </div>
    <div v-else-if="state === 'notAsked'" :data-testid="testid('notAsked')" class="text-[12px] text-fg-mute">
      {{ message ?? 'Not configured yet.' }}
    </div>
    <div v-else-if="state === 'denied'" :data-testid="testid('denied')" class="text-[12px] rounded-md px-3 py-2 bg-warning-soft text-warning-text">
      {{ message ?? 'This read was refused (HTTP 403).' }}
    </div>
    <div v-else-if="state === 'empty'" :data-testid="testid('empty')" class="text-[12px] text-fg-mute">
      {{ message ?? 'Nothing here yet.' }}
    </div>
    <div v-else-if="state === 'failed'" :data-testid="testid('failed')" class="text-[12px] rounded-md px-3 py-2 bg-danger-soft text-danger-text" role="alert">
      {{ message ?? 'This panel could not load.' }}
    </div>
    <div v-else class="min-w-0">
      <slot />
    </div>
  </section>
</template>
```

- [ ] **Step 4: Run the test and watch it pass**

Run: `pnpm test src/features/cockpit/components/CockpitPanel.test.ts`

- [ ] **Step 5: Add the four panels**

`src/features/cockpit/components/AgentsPanel.vue`:

```vue
<script setup lang="ts">
import type { PanelState } from '../panelState'
import { computed } from 'vue'
import { useAgents } from '@/features/agents'
import CockpitPanel from './CockpitPanel.vue'

// autoStart: false — useAgents holds module-level state, so this is the stream
// App.vue already started, never a second one.
const { agents, isLoading, error } = useAgents({ autoStart: false })

// No denied and no notAsked: the agent stream is not gated, and it streams
// from mount. Rendering branches that cannot occur would make the five-state
// assertion meaningless for this panel.
const state = computed<PanelState>(() => {
  if (error.value)
    return 'failed'
  if (isLoading.value)
    return 'loading'
  return agents.value.length === 0 ? 'empty' : 'ready'
})
</script>

<template>
  <CockpitPanel id="agents" title="Agents" :state="state" :message="error ?? 'No agent is running right now.'">
    <ul class="flex flex-col gap-1.5">
      <li v-for="a in agents.slice(0, 6)" :key="a.sessionId" class="flex items-center justify-between gap-2 text-[12px] min-w-0" :data-testid="`cockpit-agent-${a.sessionId}`">
        <span class="truncate text-fg">{{ a.projectName }}</span>
        <span class="shrink-0 text-fg-mute">{{ a.status }}</span>
      </li>
    </ul>
  </CockpitPanel>
</template>
```

`src/features/cockpit/components/PipelinePanel.vue` — identical shape over `useTasks({ autoStart: false })`, grouping `tasks` by `currentStage` and listing the counts:

```vue
<script setup lang="ts">
import type { PanelState } from '../panelState'
import { computed } from 'vue'
import { useTasks } from '@/features/pipeline'
import { STAGE_LABELS } from '@/utils/stageLabels'
import CockpitPanel from './CockpitPanel.vue'

// tasksByStageMap already exists on useTasks — do not re-derive the grouping
// here. autoStart: false, same singleton rule as the agents stream.
const { tasksByStageMap, isLoading, error } = useTasks({ autoStart: false })

const byStage = computed(() =>
  Object.entries(tasksByStageMap.value)
    .map(([stage, list]) => [stage, list?.length ?? 0] as const)
    .filter(([, count]) => count > 0)
    .sort((a, b) => b[1] - a[1]))

const state = computed<PanelState>(() => {
  if (error.value)
    return 'failed'
  if (isLoading.value)
    return 'loading'
  return byStage.value.length === 0 ? 'empty' : 'ready'
})
</script>

<template>
  <CockpitPanel id="pipeline" title="Pipeline" :state="state" :message="error ?? 'No task is in the pipeline.'">
    <ul class="flex flex-col gap-1.5">
      <li v-for="[stage, count] in byStage" :key="stage" class="flex items-center justify-between gap-2 text-[12px]" :data-testid="`cockpit-stage-${stage}`">
        <span class="truncate text-fg">{{ STAGE_LABELS[stage] ?? stage }}</span>
        <span class="shrink-0 text-fg-mute">{{ count }}</span>
      </li>
    </ul>
  </CockpitPanel>
</template>
```

**Two things this file needs that do not exist yet.** `useTasks` is *not* exported from the `@/features/pipeline` barrel — `src/features/pipeline/index.ts` exports only `usePipelineConfig` and `useProjectPipelineConfig`, and a deep import from the cockpit feature is an ESLint error — so add `export * from './composables/useTasks'` to it. And `STAGE_LABELS` (`src/utils/stageLabels.ts:3`) is typed `Record<PipelineStage, string>`, so a stage string outside the union has no entry; the `??` fallback to the raw stage is what covers a free-form stage such as `plan_review`.

`src/features/cockpit/components/RoutinesPanel.vue` and `MemoryPanel.vue` both read `useResources()`, which lives in `src/features/settings/composables/useResources.ts`. That is a cross-feature import, so **add `src/features/settings/index.ts`**:

```ts
export * from './composables/useResources'
```

and import it as `import { useResources } from '@/features/settings'`.

`RoutinesPanel.vue`:

```vue
<script setup lang="ts">
import type { PanelState } from '../panelState'
import { computed, onMounted } from 'vue'
import { useResources } from '@/features/settings'
import CockpitPanel from './CockpitPanel.vue'

// A fresh useResources per panel: it is not a singleton, and each panel asks
// for a different kind. Both fire one request on mount.
const { resources, loading, error, denied, fetchResources } = useResources()
onMounted(() => void fetchResources({ kind: 'routine' }))

// kind=routine is deliberately ungated server-side (api/resources/handler.go
// gates memory_space only), so `denied` is carried anyway rather than dropped:
// if that route is ever gated, this panel reports the refusal instead of
// drawing it as an empty list.
const state = computed<PanelState>(() => {
  if (loading.value)
    return 'loading'
  if (denied.value)
    return 'denied'
  if (error.value)
    return 'failed'
  return resources.value.length === 0 ? 'empty' : 'ready'
})
</script>

<template>
  <CockpitPanel
    id="routines"
    title="Routines"
    :state="state"
    :message="denied ?? error ?? 'No routine is scheduled.'"
  >
    <ul class="flex flex-col gap-1.5">
      <li v-for="r in resources.slice(0, 6)" :key="r.id" class="flex items-center justify-between gap-2 text-[12px] min-w-0" :data-testid="`cockpit-routine-${r.id}`">
        <span class="truncate text-fg">{{ r.name }}</span>
        <span class="shrink-0 text-fg-mute">{{ r.state }}</span>
      </li>
    </ul>
  </CockpitPanel>
</template>
```

`MemoryPanel.vue` is the same file with `kind: 'memory_space'`, `id="memory"`, `title="Memory"`, and the empty message `'No memory space is defined in this scope.'`. Its `denied` branch is the one that really fires: `kind=memory_space` gates on `memory.read`, so on a fresh install this panel is denied and must say so rather than reporting an empty store.

- [ ] **Step 6: Compose the cockpit and make it the landing view**

Create `src/features/cockpit/components/CockpitView.vue`:

```vue
<script setup lang="ts">
import { defineAsyncComponent } from 'vue'
import AgentsPanel from './AgentsPanel.vue'
import MemoryPanel from './MemoryPanel.vue'
import PipelinePanel from './PipelinePanel.vue'
import RoutinesPanel from './RoutinesPanel.vue'

// GitHub is the only panel that reaches a third party; loading it on demand
// keeps its composable out of the first-load chunk, matching how App.vue
// splits the heavy views.
const GitHubPanel = defineAsyncComponent(() => import('./GitHubPanel.vue'))
</script>

<template>
  <div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3" data-testid="cockpit">
    <AgentsPanel />
    <PipelinePanel />
    <RoutinesPanel />
    <MemoryPanel />
    <GitHubPanel />
  </div>
</template>
```

`GitHubPanel.vue` arrives in Task 8. To keep this task's gate green on its own, create it now as a two-line stub that renders `<CockpitPanel id="github" title="GitHub" state="loading" />` and replace it wholesale in Task 8 — never leave a stub uncommitted across two tasks, and never ship it as the final state.

In `src/composables/useViewState.ts`:

```ts
export type ActiveView = 'cockpit' | 'dashboard' | 'workflows' | 'pipeline' | 'cost' | 'schedules' | 'eval'
```

```ts
const ACTIVE_VIEWS: ActiveView[] = ['cockpit', 'dashboard', 'workflows', 'pipeline', 'cost', 'schedules', 'eval']
```

and in `readInitial`, change the two `view = 'dashboard'` fallbacks and the `ls?.setItem('agent-active-view', 'dashboard')` repair to `'cockpit'`. Leave every `case` inside the legacy `agent-view-mode` migration alone: a stored `'list'`/`'cards'`/`'config-explorer'` meant the agent grid, and it still does.

In `src/utils/navConfig.ts`, put Cockpit first in `NAV_ITEMS`:

```ts
  { view: 'cockpit', label: 'Cockpit', icon: '◈', group: 'Monitor' },
```

and add to `src/utils/navConfig.test.ts`:

```ts
  it('cockpit is the first Monitor item and has a title', () => {
    expect(NAV_ITEMS[0].view).toBe('cockpit')
    expect(viewTitle('cockpit')).toBe('Cockpit')
  })
```

In `src/App.vue`, add the branch as the first view in the chain, next to `DashboardView`:

```ts
const CockpitView = defineAsyncComponent(() => import('@/features/cockpit/components/CockpitView.vue'))
```

```html
        <CockpitView v-if="activeView === 'cockpit'" />
```

placed **after** the existing `v-if="isLoading && activeView === 'dashboard'"` / `v-else-if="error"` pair, as the first arm of the `v-else` chain — so the cockpit is not blanked by an agent-stream error the way the dashboard is. Change `<template v-else-if="activeView === 'dashboard'">` accordingly: the chain becomes `isLoading&&dashboard` → `error` → `cockpit` → `dashboard` → the rest.

The `+ New Agent` / `+ New Task` topbar CTAs stay bound to `dashboard` and `pipeline`. **The spec is silent on a cockpit CTA; the decision here is no CTA on the cockpit** — each panel is a link into the view that owns the action, and a spawn button on a summary screen would duplicate the one the Agents panel links to.

- [ ] **Step 7: Fix the two E2E specs the new default breaks**

`tests/e2e/dashboard.spec.ts` and `tests/e2e/workflows.spec.ts` both assert the landing heading is `Dashboard` (`dashboard.spec.ts:47`, `workflows.spec.ts:29`). Both now land on the cockpit.

In `tests/e2e/dashboard.spec.ts`, extend `clearShellStorage`'s init script so the dashboard suite starts on the dashboard:

```ts
    localStorage.setItem('agent-active-view', 'dashboard')
```

and replace the `dashboard is the default view` test with two:

```ts
  test('the dashboard view still renders under its own heading', async ({ page }) => {
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Dashboard')
  })
```

and, in a describe of its own that does *not* set the storage key:

```ts
test.describe('landing view', () => {
  test('cockpit is the default view on a first visit', async ({ page }) => {
    await stubAuthDisabled(page)
    await page.goto('/')
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Cockpit')
    await expect(page.getByTestId('cockpit')).toBeVisible()
  })
})
```

In `tests/e2e/workflows.spec.ts`, replace line 29's assertion with a navigation, since the test only needed a mounted shell:

```ts
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Cockpit')
```

- [ ] **Step 8: Run the gate and the E2E suite**

```bash
pnpm lint && pnpm typecheck && pnpm test
pnpm test:e2e
```

- [ ] **Step 9: Commit**

```bash
git add src tests/e2e
git commit -m "feat(cockpit): make the cockpit the landing view"
```

---

### Task 8: The GitHub panel and the GitHub settings form

**Files:**
- Create: `src/features/cockpit/composables/useGitHubSummary.ts`
- Replace: `src/features/cockpit/components/GitHubPanel.vue` (the Task 7 stub)
- Create: `src/features/cockpit/components/GitHubPanel.test.ts`
- Create: `src/features/settings/components/GitHubSettings.vue`
- Modify: `src/features/settings/components/ApiKeySettings.vue`

**Interfaces:**
- Produces:
  - `interface GitHubPullRequest { number: number; title: string; author: string; url: string; draft: boolean; updatedAt: string }`
  - `interface GitHubRepoSummary { repo: string; pullRequests: GitHubPullRequest[]; error?: string }`
  - `useGitHubSummary(): { repos: Ref<GitHubRepoSummary[]>; loading: Ref<boolean>; error: Ref<string | null>; denied: Ref<string | null>; unconfigured: Ref<boolean>; fetchSummary: () => Promise<void> }`
- Consumes: `errorMessage`, `readErrorMessage` from `@/utils/errorMessage`; `useSettings` from `@/features/settings/composables/useSettings`.

The DTO names mirror `server/internal/api/github/handler.go`'s `repoSummary`/`pullRequestView` json tags exactly — that handler is the single source of the wire shape, and a rename on one side without the other is the drift a fixture-shaped test cannot see.

- [ ] **Step 1: Write the failing test**

Create `src/features/cockpit/components/GitHubPanel.test.ts`:

```ts
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import GitHubPanel from './GitHubPanel.vue'

function stubFetch(status: number, body: unknown) {
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })))
}

afterEach(() => vi.unstubAllGlobals())

const others = (state: string) => ['loading', 'notAsked', 'denied', 'empty', 'failed'].filter(s => s !== state)

async function mountPanel() {
  const wrapper = mount(GitHubPanel)
  await flushPromises()
  return wrapper
}

function expectOnly(wrapper: ReturnType<typeof mount>, state: string) {
  expect(wrapper.find(`[data-testid="cockpit-github-${state}"]`).exists()).toBe(true)
  for (const other of others(state))
    expect(wrapper.findAll(`[data-testid="cockpit-github-${other}"]`)).toHaveLength(0)
}

describe('gitHubPanel', () => {
  it('reports a 503 as not configured, never as an empty repository list', async () => {
    stubFetch(503, { error: 'github is not configured' })
    expectOnly(await mountPanel(), 'notAsked')
  })

  it('reports a 403 as denied, with the server reason, and shows no rows', async () => {
    stubFetch(403, { error: 'capability denied: github.read' })
    const wrapper = await mountPanel()
    expectOnly(wrapper, 'denied')
    expect(wrapper.get('[data-testid="cockpit-github-denied"]').text()).toContain('capability denied: github.read')
  })

  it('tells a confirmed-empty answer apart from a refusal', async () => {
    stubFetch(200, { repos: [{ repo: 'lx-wnk/agent-dashboard', pullRequests: [] }] })
    expectOnly(await mountPanel(), 'empty')
  })

  it('reports a 500 as failed, which is not the same as denied', async () => {
    stubFetch(500, { error: 'upstream exploded' })
    const wrapper = await mountPanel()
    expectOnly(wrapper, 'failed')
    expect(wrapper.get('[data-testid="cockpit-github-failed"]').text()).toContain('upstream exploded')
  })

  it('renders the pull requests it was given', async () => {
    stubFetch(200, {
      repos: [{
        repo: 'lx-wnk/agent-dashboard',
        pullRequests: [{ number: 42, title: 'Add the cockpit', author: 'lx-wnk', url: 'https://example.test/42', draft: false, updatedAt: '2026-09-01T10:00:00Z' }],
      }],
    })
    const wrapper = await mountPanel()
    for (const state of ['loading', 'notAsked', 'denied', 'empty', 'failed'])
      expect(wrapper.findAll(`[data-testid="cockpit-github-${state}"]`)).toHaveLength(0)
    expect(wrapper.get('[data-testid="cockpit-github-pr-42"]').text()).toContain('Add the cockpit')
  })
})
```

- [ ] **Step 2: Run the test and watch it fail**

Run: `pnpm test src/features/cockpit/components/GitHubPanel.test.ts`
Expected: four of the five fail because the Task 7 stub is permanently `loading`.

- [ ] **Step 3: Write the composable**

Create `src/features/cockpit/composables/useGitHubSummary.ts`:

```ts
import { ref } from 'vue'
import { errorMessage, readErrorMessage } from '@/utils/errorMessage'

// Mirrors pullRequestView / repoSummary in server/internal/api/github/handler.go.
// That handler owns the wire shape; these names must match its json tags.
export interface GitHubPullRequest {
  number: number
  title: string
  author: string
  url: string
  draft: boolean
  updatedAt: string
}

export interface GitHubRepoSummary {
  repo: string
  pullRequests: GitHubPullRequest[]
  error?: string
}

// States only what is known. The route maps every Gate.Authorize failure to
// 403 — missing grant, rate limit, unreadable grant store alike — so the
// fallback names no cause, matching useResources' DENIED_FALLBACK.
const DENIED_FALLBACK = 'The GitHub route refused this read (HTTP 403) without giving a reason.'

export function useGitHubSummary() {
  const repos = ref<GitHubRepoSummary[]>([])
  const loading = ref(true)
  const error = ref<string | null>(null)
  const denied = ref<string | null>(null)
  // 503 from the route means github.token/github.repos are unset: the request
  // was answered, but nothing was ever asked of GitHub. Held apart from
  // `error` because the fix is different — configure it, not repair it.
  const unconfigured = ref(false)

  async function fetchSummary(): Promise<void> {
    loading.value = true
    error.value = null
    denied.value = null
    unconfigured.value = false
    try {
      const res = await fetch('/api/github/summary')
      if (res.status === 503) {
        unconfigured.value = true
        repos.value = []
        return
      }
      if (res.status === 403) {
        denied.value = await readErrorMessage(res, DENIED_FALLBACK)
        repos.value = []
        return
      }
      if (!res.ok)
        throw new Error(await readErrorMessage(res, `Failed to load the GitHub summary (HTTP ${res.status})`))
      const body = await res.json() as { repos?: GitHubRepoSummary[] }
      repos.value = body.repos ?? []
    }
    catch (e) {
      // Cleared on failure: leaving the previous answer on screen under a
      // failure notice would misreport what GitHub holds now.
      repos.value = []
      error.value = errorMessage(e, 'Failed to load the GitHub summary')
    }
    finally {
      loading.value = false
    }
  }

  return { repos, loading, error, denied, unconfigured, fetchSummary }
}
```

- [ ] **Step 4: Write the panel**

Replace `src/features/cockpit/components/GitHubPanel.vue`:

```vue
<script setup lang="ts">
import type { PanelState } from '../panelState'
import { computed, onMounted } from 'vue'
import { useGitHubSummary } from '../composables/useGitHubSummary'
import CockpitPanel from './CockpitPanel.vue'

const { repos, loading, error, denied, unconfigured, fetchSummary } = useGitHubSummary()
onMounted(() => void fetchSummary())

const pullRequests = computed(() => repos.value.flatMap(r => r.pullRequests.map(pr => ({ ...pr, repo: r.repo }))))

// The order matters and is the whole five-state rule: a refusal is checked
// before an empty answer, so a denied read can never be drawn as "no open
// pull requests", and "not configured" is checked before both, so an
// unconfigured install never looks like a healthy quiet one.
const state = computed<PanelState>(() => {
  if (loading.value)
    return 'loading'
  if (unconfigured.value)
    return 'notAsked'
  if (denied.value)
    return 'denied'
  if (error.value)
    return 'failed'
  return pullRequests.value.length === 0 ? 'empty' : 'ready'
})

const message = computed(() => {
  if (unconfigured.value)
    return 'Set github.token and github.repos in Settings → GitHub to switch this on.'
  return denied.value ?? error.value ?? 'No open pull request in the configured repositories.'
})
</script>

<template>
  <CockpitPanel id="github" title="GitHub" :state="state" :message="message">
    <ul class="flex flex-col gap-1.5">
      <li
        v-for="pr in pullRequests.slice(0, 8)"
        :key="`${pr.repo}#${pr.number}`"
        class="flex items-center justify-between gap-2 text-[12px] min-w-0"
        :data-testid="`cockpit-github-pr-${pr.number}`"
      >
        <a :href="pr.url" target="_blank" rel="noopener noreferrer" class="truncate text-fg hover:text-accent">
          {{ pr.title }}
        </a>
        <span class="shrink-0 text-fg-mute">{{ pr.repo }}#{{ pr.number }}</span>
      </li>
    </ul>
  </CockpitPanel>
</template>
```

- [ ] **Step 5: Run the test and watch it pass**

Run: `pnpm test src/features/cockpit/components/GitHubPanel.test.ts`

- [ ] **Step 6: Add the settings form**

Create `src/features/settings/components/GitHubSettings.vue` modelled on `ObsidianSettings.vue` — same `useSettings()` composable, same seed-once `watch(items, …)` guard, same mask-sentinel handling for the secret. The one difference is the completeness rule:

```ts
// github.token and github.repos are a required PAIR server-side
// (serverapp.buildGitHubClient) — some-but-not-all set is a state the server
// refuses to BOOT with, and the dashboard is what dies, so the save is
// blocked rather than warned about. Both empty stays allowed: that is the
// working "off" switch. github.baseURL is NOT part of the pair — it carries a
// registry default, so it is never unset.
const pairComplete = computed(() => {
  const setCount = [form.value.token, form.value.repos].filter(v => v !== '').length
  return setCount === 0 || setCount === 2
})
```

Register it in `src/features/settings/components/ApiKeySettings.vue`: add `'github'` to the `Section` union, add `{ id: 'github', icon: '⑂', label: 'GitHub' }` to `SECTIONS` (line 52-58) directly after the `obsidian` entry, import `GitHubSettings`, and add the matching render block next to the Obsidian one (line 651-653).

- [ ] **Step 7: Run the gate**

```bash
pnpm lint && pnpm typecheck && pnpm test
```

- [ ] **Step 8: Commit**

```bash
git add src/features/cockpit src/features/settings
git commit -m "feat(cockpit): show the GitHub summary and let it be configured"
```

---

### Task 9: The five states end to end, and the documentation

**Files:**
- Create: `tests/e2e/cockpit.spec.ts`
- Modify: `README.md`, `CHANGELOG.md`, `docs/guides/mcp.md`, `docs/guides/security.md`

- [ ] **Step 1: Write the failing E2E spec**

Create `tests/e2e/cockpit.spec.ts`:

```ts
import type { Page } from '@playwright/test'
import { expect, test } from '@playwright/test'
import { stubAuthDisabled, stubEmptyStream, stubJson } from './helpers'

const PANEL_STATES = ['loading', 'notAsked', 'denied', 'empty', 'failed']

/**
 * The five-state rule, asserted at the surface a user actually sees: exactly
 * one state marker per panel, never two. `toHaveCount(0)` on every sibling is
 * what makes it an assertion rather than a hope — the same shape
 * settings-gated-panels.spec.ts uses.
 */
async function expectOnlyState(page: Page, panel: string, state: string) {
  await expect(page.getByTestId(`cockpit-${panel}-${state}`)).toBeVisible()
  for (const other of PANEL_STATES.filter(s => s !== state))
    await expect(page.getByTestId(`cockpit-${panel}-${other}`)).toHaveCount(0)
}

async function openCockpit(page: Page) {
  await page.goto('/')
  await expect(page.getByTestId('cockpit')).toBeVisible()
}

test.describe('cockpit panels', () => {
  test.beforeEach(async ({ page }) => {
    await stubAuthDisabled(page)
    await stubJson(page, '/api/agents', [])
    await stubEmptyStream(page, '/api/agents/stream')
    await stubEmptyStream(page, '/api/tasks/stream')
    await stubJson(page, '/api/config', { mcpServerName: 'agent-dashboard', mcpEndpoint: '/api/mcp' })
    await stubJson(page, '/api/spawners', [])
  })

  test('GitHub: unconfigured is not an empty repository list', async ({ page }) => {
    await stubJson(page, '/api/github/summary', { error: 'github is not configured' }, 503)
    await openCockpit(page)
    await expectOnlyState(page, 'github', 'notAsked')
  })

  test('GitHub: denied renders the server reason, not an empty list', async ({ page }) => {
    await stubJson(page, '/api/github/summary', { error: 'capability denied: github.read' }, 403)
    await openCockpit(page)
    await expectOnlyState(page, 'github', 'denied')
    await expect(page.getByTestId('cockpit-github-denied')).toContainText('github.read')
  })

  test('GitHub: a confirmed-empty answer is distinct from a refusal and from a failure', async ({ page }) => {
    await stubJson(page, '/api/github/summary', { repos: [{ repo: 'lx-wnk/agent-dashboard', pullRequests: [] }] })
    await openCockpit(page)
    await expectOnlyState(page, 'github', 'empty')
  })

  test('GitHub: a 500 is failed, never denied', async ({ page }) => {
    await stubJson(page, '/api/github/summary', { error: 'upstream exploded' }, 500)
    await openCockpit(page)
    await expectOnlyState(page, 'github', 'failed')
  })

  test('Memory: a memory.read refusal is reported, not drawn as an empty store', async ({ page }) => {
    await page.route(/\/api\/resources(\?.*)?$/, async (route) => {
      const kind = new URL(route.request().url()).searchParams.get('kind')
      if (kind === 'memory_space') {
        await route.fulfill({ status: 403, contentType: 'application/json', body: JSON.stringify({ error: 'memory.read is not granted in this scope' }) })
        return
      }
      await route.fulfill({ status: 200, contentType: 'application/json', body: '[]' })
    })
    await stubJson(page, '/api/github/summary', { error: 'github is not configured' }, 503)
    await openCockpit(page)

    await expectOnlyState(page, 'memory', 'denied')
    await expect(page.getByTestId('cockpit-memory-denied')).toContainText('memory.read is not granted in this scope')
    // The sibling panel reading the same route with a different kind must be
    // unaffected — one refusal must not blank the cockpit.
    await expectOnlyState(page, 'routines', 'empty')
  })

  test('the cockpit does not replace the dashboard, it sits beside it', async ({ page }) => {
    await stubJson(page, '/api/github/summary', { error: 'github is not configured' }, 503)
    await openCockpit(page)
    await page.getByRole('navigation', { name: 'Primary' }).getByRole('button', { name: 'Dashboard' }).click()
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Dashboard')
    await expect(page.getByTestId('cockpit')).toHaveCount(0)
  })
})
```

- [ ] **Step 2: Run it and watch it pass (or fix what it finds)**

Run: `pnpm test:e2e tests/e2e/cockpit.spec.ts`

This spec is written last on purpose: Tasks 7 and 8 already made it passable, so a failure here is a real integration defect — a panel whose states overlap, or a testid that does not match the shell's. Fix the component, never the assertion.

- [ ] **Step 3: Update the documentation, in the same change**

- `README.md`: in the **Extend** paragraph (line 48), after the Obsidian sentence, add a GitHub sentence in the same voice — registered as a resource-registry Application (`server/internal/apps/github`) with four gated capabilities, `github.merge` in class `spend` so it is denied outright without an explicit grant, configured from **Settings → GitHub** (`github.token`, `github.repos`, `github.baseURL`), reachable at `GET /api/github/summary`, `GET /api/github/search`, `POST /api/github/comment`, `POST /api/github/merge` and as the four `github_*` MCP tools. Add a **GitHub** section after the **Obsidian vault** section (line 144) with the concrete grant commands:

```
agent-dashboard grants add github.read --pattern '*' --scope global --mode allow
agent-dashboard grants add github.search --pattern '*' --scope global --mode allow
```

and a sentence saying that `github.comment` and `github.merge` are deliberately not in that list. Also record the GHE-on-LAN limitation from Task 3.
- `CHANGELOG.md`: one `### Added` entry under `## [Unreleased]`, in the file's existing narrative voice, covering the cockpit landing view and the GitHub application. Say what the MLP criterion was and that GitHub went through the kernel unchanged — or, if any task in this plan turned out to need a kernel change, say exactly what it was, because that is the finding (spec §7).
- `docs/guides/mcp.md`: add `github:read`, `github:write` and `github:merge` to the scope table (lines 26-27) and the tool listing (lines 44-46), including the fact that `github:write` does **not** imply `github:merge`.
- `docs/guides/security.md`: extend the **Server** enforcement-point row (line 86) with the four GitHub HTTP routes and four MCP tools as production callers of `memory.Gate.Authorize`, and add a short subsection after **Obsidian's TLS trust model** covering the token's blast radius (a fine-grained PAT scoped to the listed repositories), the repo allow-list running before the gate, and why `github.merge` is class `spend`.

Verify every doc claim against the code before writing it. A stale doc is a defect, not a follow-up.

- [ ] **Step 4: Run the full gate**

```bash
pnpm lint && pnpm typecheck && pnpm test
pnpm test:e2e
cd server && go build ./... && go vet ./... && gofmt -l ./internal ./serverapp
GOTOOLCHAIN=go1.26.6 task lint
git status --short   # server/internal/db/ent/ MUST be clean
```

- [ ] **Step 5: Commit**

```bash
git add tests/e2e/cockpit.spec.ts README.md CHANGELOG.md docs/guides/mcp.md docs/guides/security.md
git commit -m "docs: describe the cockpit and the GitHub application"
```

---

## Self-Review

**Spec coverage.**

| Spec section | Task |
|---|---|
| §3 D1 — GitHub is an in-server module | Task 2 (`apps/github`, `Register`), Task 4 (boot wiring) |
| §3 D2 — four capabilities, `merge` is class `spend` | Task 2 (`capabilityDecls` + the class assertions), Tasks 5 and 6 (the deny-with-no-grant tests on both surfaces) |
| §3 D3 — a fine-grained PAT in encrypted settings | Task 2 (`github.token` as `Secret: true`), Task 4 (`Service.Secret`, the pair rule), Task 8 (the masked form field) |
| §3 D4 — allow-list checked before the gate | Task 3 (`AllowsRepo`, `ErrRepoNotAllowed`, the client's own refusal), Tasks 5 and 6 (`allow()`/`authorize()`, and the tests that grant merge globally and are still refused) |
| §3 D5 — cockpit is the landing view, dashboard stays | Task 1 (the move), Task 7 (`ActiveView`, nav, default, and the E2E test that the dashboard still works) |
| §3 D6 — no placeholder panels | Task 7 — five panels, all with a real source; nothing renders "not connected yet" |
| §4.1 the application | Tasks 2, 3, 4 |
| §4.2 reaching it — both surfaces | Tasks 5 and 6, plus `TestEveryGitHubCapabilityIsOnBothSurfaces` |
| §4.3 the cockpit and the five states | Tasks 7 and 8, `CockpitPanel.vue` as their single owner |
| §4.4 order of work | Task order, step 1 first and alone |
| §5 out of scope | Nothing implements Slack/Jira/Mail/Calendar, the `agent_session` context level, or any path around the merge deny |
| §6 testing matrix | Row 1 → Tasks 5, 6. Row 2 → Tasks 5, 6. Row 3 → Tasks 3, 5, 6. Row 4 → Task 6's parity test plus the paired deny/allow tests. Row 5 → Tasks 3, 4, 5, 6. Row 6 → Task 4. Row 7 → Tasks 7, 8, 9. Row 8 → Task 1 step 7 |
| §7 risks | Merge without a human → Task 2's class + Task 3's allow-list. Token blast radius → Tasks 2, 3, 9's docs. `App.vue` restructure → Task 1 alone. Kernel needs a GitHub-specific change → Task 9 step 3 requires the CHANGELOG to say so if it happened |

**Gaps, stated rather than hidden.**

- **Design tokens (§4.3, second paragraph).** The spec calls for `--bg`, `--line`, `--accent`, `--now`, IBM Plex Sans and Mono from the Claude Desktop mock. No task adopts a new type stack or new CSS variables: the panels use the existing `bg-card`, `border-line`, `text-fg`, `text-fg-mute`, `text-accent` tokens the rest of the app already uses. Introducing a second design system alongside the working one is a change of a different kind and a different size from this slice, and would have to touch every existing view to avoid two visual languages in one shell. **Recorded as out of scope here, deliberately.**
- **`--now` and the RUBRIC "dense edge tiles, quiet centre" layout.** Same reason. The layout used is the app's existing responsive card grid.
- **The five-state rule is narrowed, and the narrowing is written down.** The spec asks for five states per panel. Task 7's table names, per panel, which states are structurally unreachable and why (the Agents and Pipeline panels are not gated and stream from mount; `kind=routine` is deliberately ungated server-side). The testable requirement is mutual exclusion — exactly one state marker, every sibling at count 0 — which is what "a panel that collapses any two is a defect" actually means. The GitHub panel does reach all five.

**Places the spec disagreed with the code, and the plan followed the code.** All three are in the verification table above with `file:line`: `CallerResolver` does not exist on this branch (§4.2); the settings "trio" is a pair because `github.baseURL` carries a default (§4.1); and §4.2's single HTTP route would have left two capabilities MCP-only, so there are four.

**Decisions made because the spec was silent.** MCP scope names and their implications (three scopes; `github:write` does not imply `github:merge`). The summary's content (per repository, the five most recently updated open pull requests, one call per repository, a per-repository `error` so one failing repository does not blank the panel). `validation.SafeDialContext` as the client's dialer, and the GHE-on-LAN limitation that follows. `github.search` bounded by an appended `repo:` qualifier per configured repository on both surfaces. No topbar CTA on the cockpit. The Cockpit nav item sits first in the Monitor group.

**Placeholder scan.** One placeholder exists and it is scoped to a single task: Task 7 step 6 creates `GitHubPanel.vue` as a permanently-`loading` stub so that task's gate is green on its own, and Task 8 step 4 replaces the file wholesale. It must not survive Task 8. There is no `t.Skip`, no `it.todo`, and no "similar to Task N" step in this plan.

**Type consistency across tasks.** `github.CapabilityDecl{Name, Class, Reversible}` (Task 2) is iterated with those field names by Task 6's parity test. `github.Config{Token, BaseURL, Repos, AllowLoopback}` (Task 3) is constructed with those names in Tasks 4, 5 and 6. `ErrRepoNotAllowed` (Task 3) is asserted with `errors.Is` in Task 3 and matched by message in Tasks 5 and 6 — the handler and the tool both wrap it in their own text, so the sentinel is checked only where it is returned directly. `(*Client).AllowsRepo(string) bool` and `(*Client).Repos() []string` (Task 3) are called with those signatures in Tasks 5 and 6. `buildGitHubClient(ctx, *settings.Service) (*github.Client, error)` (Task 4) is called that way in `di.go`. The handler's `repoSummary{repo, pullRequests, error}` and `pullRequestView{number, title, author, url, draft, updatedAt}` json tags (Task 5) are the field names `GitHubRepoSummary` and `GitHubPullRequest` carry in Task 8, and the shapes Task 9's E2E stubs post. `GitHubDeps{Client, Gate}` (Task 6) is constructed with those names in `di_mcp.go`. `PanelState` and `PANEL_STATES` (Task 7) are imported by every panel and by both the component tests and the E2E spec, which derive their sibling lists from the same union.
