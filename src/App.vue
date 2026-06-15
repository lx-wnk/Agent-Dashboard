<script setup lang="ts">
import type { Agent, PipelineTask } from './types'
import { computed, defineAsyncComponent, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import AgentCardGrid from './components/AgentCardGrid.vue'
import AgentModal from './components/AgentModal.vue'
import AgentTable from './components/AgentTable.vue'
import AgentTriageBand from './components/AgentTriageBand.vue'
import ApiKeySettings from './components/ApiKeySettings.vue'
import AutoApprovingStrip from './components/AutoApprovingStrip.vue'
import BacklogForm from './components/BacklogForm.vue'
import EmptyAgentState from './components/EmptyAgentState.vue'
import LoginPage from './components/LoginPage.vue'
import SessionList from './components/SessionList.vue'
import AppShell from './components/shell/AppShell.vue'
import AppSidebar from './components/shell/AppSidebar.vue'
import AppStatusBar from './components/shell/AppStatusBar.vue'
import AppTopbar from './components/shell/AppTopbar.vue'
import ChannelScriptCallout from './components/shell/ChannelScriptCallout.vue'
import DashboardToolbar from './components/shell/DashboardToolbar.vue'
import SkeletonCard from './components/shell/SkeletonCard.vue'
import SpawnDialog from './components/SpawnDialog.vue'
import SpotlightSearch from './components/SpotlightSearch.vue'
import AppModal from './components/ui/AppModal.vue'
import AppModalHeader from './components/ui/AppModalHeader.vue'
import { useAgents } from './composables/useAgents'
import { useInstallPrompt } from './composables/useInstallPrompt'
import { useNow } from './composables/useNow'
import { usePendingPermissions } from './composables/usePendingPermissions'
import { usePermissionResolve } from './composables/usePermissionResolve'
import { usePWA } from './composables/usePWA'
import { useServerConfig } from './composables/useServerConfig'
import { useSidebar } from './composables/useSidebar'
import { useSpawners } from './composables/useSpawners'
import { useTasks } from './composables/useTasks'
import { useTheme } from './composables/useTheme'
import { useTodayCost } from './composables/useTodayCost'
import { useUser } from './composables/useUser'
import { useViewState } from './composables/useViewState'
import { groupAgents, sortAgents } from './utils/agentGroup'
import { needsAttention } from './utils/attention'
import { formatCost, formatTokens, secondsSince, totalTokenCount } from './utils/format'
import { friendlyProjectName } from './utils/friendlyProjectName'

// F-PERF-019: top-level heavy views loaded on demand — each becomes its own chunk
const CostAnalyticsView = defineAsyncComponent(() => import('./components/CostAnalyticsView.vue'))
const EvalView = defineAsyncComponent(() => import('./components/EvalView.vue'))
const PipelineBoard = defineAsyncComponent(() => import('./components/PipelineBoard.vue'))
const WorkflowsView = defineAsyncComponent(() => import('./components/WorkflowsView.vue'))
// Heavy modal loaded on demand — split into its own chunk (includes DependencyGraph + StageCostWaterfall).
const TaskModal = defineAsyncComponent(() => import('./components/TaskModal.vue'))
// Modal/panel components that drag in marked + dompurify (RefinementChat) and diff (EditGateModal) —
// load on demand so those libs stay out of the first-load entry chunk.
const RefinementChat = defineAsyncComponent(() => import('./components/RefinementChat.vue'))
const EditGateModal = defineAsyncComponent(() => import('./components/EditGateModal.vue'))

const { user, authEnabled, loaded, loadUser } = useUser()
const { homedir, loadServerConfig } = useServerConfig()
const showLogin = computed(() => authEnabled.value && !user.value)
const { needsRefresh, updateSW } = usePWA()
const { canInstall, promptInstall } = useInstallPrompt()
const { theme, toggleTheme } = useTheme()

const { activeView, dashboardLayout, dashboardSort, dashboardGroup, dashboardProject, dashboardSpawner } = useViewState()
const { handleShortcut: handleSidebarShortcut } = useSidebar()
const { resolveAgent, approveAll } = usePermissionResolve()

// F-UIUX-011: 5 s default duration; hover pause/resume keeps toast visible while pointer rests on it
const TOAST_DURATION_MS = 5000
const toastMessage = ref<string | null>(null)
let toastTimer: ReturnType<typeof setTimeout> | null = null
let toastPaused = false

onMounted(() => {
  loadUser()
  void loadServerConfig()
})

onUnmounted(() => {
  if (toastTimer)
    clearTimeout(toastTimer)
})

const { agents, costTrend, filteredAgents, attentionAgents, attentionCount, selectedAgent, isLoading, error, searchQuery, selectAgent, startStream: startAgents } = useAgents({ autoStart: false })
const { tasks, selectedTask, selectTask, startStream: startTasks } = useTasks({ autoStart: false })
const { items: permissionItems, approve: approvePermission, deny: denyPermission } = usePendingPermissions(tasks)
const combinedAttentionCount = computed(() => attentionCount.value + permissionItems.value.length)
// Today's persisted spend — reuses the shared cost-summary logic so the footer
// and Cost view agree. Distinct from totalCost (cost of agents running now).
const { todayUsd, start: startTodayCost } = useTodayCost()

// Start data streams only after auth is confirmed — avoids 401 flood while login page is shown
watch(loaded, (isLoaded) => {
  if (isLoaded && !showLogin.value) {
    startAgents()
    startTasks()
    startTodayCost()
  }
}, { immediate: true })

// Move focus to main content on view change for keyboard/screen-reader users
watch(activeView, () => {
  nextTick(() => document.getElementById('main-content')?.focus())
})

const live = computed(() => !error.value)

// Live burn-rate over the last 5 minutes, computed time-based from the
// client-side cost-trend ring buffer (see useAgents). Falls back to null until
// there are at least two samples spanning a measurable interval.
const BURN_WINDOW_MS = 5 * 60 * 1000
const costDelta = computed(() => {
  const pts = costTrend.value
  if (pts.length < 2)
    return null
  const last = pts[pts.length - 1]
  const cutoff = last.t - BURN_WINDOW_MS
  // Earliest sample at or after the cutoff is the window baseline.
  const baseline = pts.find(p => p.t >= cutoff) ?? pts[0]
  if (baseline.t === last.t)
    return null
  return last.cost - baseline.cost
})

const totalCost = computed(() => agents.value.reduce((sum, a) => sum + a.costEstimate, 0))
const totalTokens = computed(() => agents.value.reduce((sum, a) => sum + totalTokenCount(a.tokenUsage), 0))
const _totalCostLabel = computed(() => formatCost(totalCost.value))
const _totalTokensLabel = computed(() => formatTokens(totalTokens.value))

const todayCostLabel = computed(() => (todayUsd.value === null ? '—' : formatCost(todayUsd.value)))

const { nowMs } = useNow()

const { spawners } = useSpawners()

// Agents have no direct spawner link; they reach a configured spawner through
// the task they were spawned for (task.spawnerId). Map taskId → spawnerId so the
// roster spawner filter can match. Free (un-orchestrated) agents have no task,
// so they only appear under "All spawners".
const taskSpawnerById = computed(() => {
  const m = new Map<string, string>()
  for (const t of tasks.value) {
    if (t.spawnerId)
      m.set(t.id, t.spawnerId)
  }
  return m
})

// Dashboard roster: project + spawner filter → sort → optional grouping. Project
// options list every known project (pre-filter) so the dropdown stays stable.
const rosterAgents = computed(() => {
  let base = filteredAgents.value
  if (dashboardProject.value !== 'all')
    base = base.filter(a => a.projectName === dashboardProject.value)
  if (dashboardSpawner.value !== 'all')
    base = base.filter(a => a.pipelineTaskId != null && taskSpawnerById.value.get(a.pipelineTaskId) === dashboardSpawner.value)
  return sortAgents(base, dashboardSort.value, nowMs.value)
})
const rosterGroups = computed(() => groupAgents(rosterAgents.value, dashboardGroup.value))
const _rosterAttentionCount = computed(() =>
  rosterAgents.value.filter(a => needsAttention(a, secondsSince(a.lastActivity, nowMs.value))).length,
)
const projectOptions = computed(() => [
  { value: 'all', label: 'All projects' },
  ...[...new Set(agents.value.map(a => a.projectName))].sort().map(n => ({ value: n, label: friendlyProjectName(n) })),
])
const spawnerOptions = computed(() => [
  { value: 'all', label: 'All spawners' },
  ...spawners.value.map(s => ({ value: s.id, label: s.name })),
])

const autoApprovingStrip = ref<InstanceType<typeof AutoApprovingStrip> | null>(null)

const showSpawnDialog = ref(false)
const activeConceptTask = ref<PipelineTask | null>(null)
const showRefinementChat = ref(false)
const showBacklogForm = ref(false)
const showSessions = ref(false)
const showSettings = ref(false)

function openNewTask() {
  showBacklogForm.value = true
}

function onCreateTaskAndRefine(task: PipelineTask) {
  showBacklogForm.value = false
  activeConceptTask.value = task
  showRefinementChat.value = true
}

function startToastTimer() {
  if (toastTimer)
    clearTimeout(toastTimer)
  toastTimer = setTimeout(() => {
    if (!toastPaused)
      toastMessage.value = null
  }, TOAST_DURATION_MS)
}

function pauseToast() {
  toastPaused = true
  if (toastTimer)
    clearTimeout(toastTimer)
}

function resumeToast() {
  toastPaused = false
  if (toastMessage.value)
    startToastTimer()
}

function showToast(msg: string) {
  toastMessage.value = msg
  toastPaused = false
  startToastTimer()
}

// Keyboard focus for the triage band: `n` cycles attention agents (wrapping).
const focusedSessionId = ref<string | null>(null)
// Guards the keyboard resolve shortcuts (a/d/⇧A) against rapid double-fire.
const kbResolving = ref(false)

function focusNextAttention() {
  const list = attentionAgents.value
  if (!list.length)
    return
  const idx = list.findIndex(a => a.sessionId === focusedSessionId.value)
  focusedSessionId.value = list[(idx + 1) % list.length].sessionId
}

function resolveFocused(outcome: 'granted' | 'denied') {
  if (kbResolving.value)
    return
  const focused = attentionAgents.value.find(a => a.sessionId === focusedSessionId.value)
  if (!focused?.pipelineTaskId || !focused.pendingPermissions?.length)
    return
  kbResolving.value = true
  void resolveAgent(focused, outcome).then((err) => {
    if (err)
      showToast(err)
  }).finally(() => { kbResolving.value = false })
}

// Shift+D theme + Cmd/Ctrl+B sidebar are global; n/a/d/⇧A/c act on the triage
// band and are gated to the dashboard with no overlay open and no field focused.
function handleKeydown(e: KeyboardEvent) {
  handleSidebarShortcut(e)

  const tag = (e.target as HTMLElement)?.tagName
  const isTyping = tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || (e.target as HTMLElement).isContentEditable
  // key normalised so CapsLock doesn't break the lower-case shortcuts
  const key = e.key.toLowerCase()

  if (key === 'd' && e.shiftKey && !e.ctrlKey && !e.metaKey && !e.altKey && !isTyping) {
    toggleTheme()
    return
  }

  // Any open modal/dialog (incl. Spotlight) suppresses the single-key shortcuts.
  const overlayOpen = selectedAgent.value || showSettings.value || showSpawnDialog.value
    || showBacklogForm.value || showSessions.value || showRefinementChat.value || activeConceptTask.value
    || document.querySelector('[role="dialog"], [aria-modal="true"]') !== null
  if (activeView.value !== 'dashboard' || overlayOpen || isTyping || e.ctrlKey || e.metaKey || e.altKey)
    return

  if (key === 'n') {
    e.preventDefault()
    focusNextAttention()
  }
  else if (key === 'a' && e.shiftKey) {
    e.preventDefault()
    if (kbResolving.value)
      return
    kbResolving.value = true
    void approveAll(attentionAgents.value.filter(a => a.pipelineTaskId && a.pendingPermissions?.length)).then((err) => {
      if (err)
        showToast(err)
    }).finally(() => { kbResolving.value = false })
  }
  else if (key === 'a') {
    e.preventDefault()
    resolveFocused('granted')
  }
  else if (key === 'd') {
    e.preventDefault()
    resolveFocused('denied')
  }
  else if (key === 'c') {
    e.preventDefault()
    dashboardLayout.value = dashboardLayout.value === 'cards' ? 'list' : 'cards'
  }
}

onMounted(() => window.addEventListener('keydown', handleKeydown))
onUnmounted(() => window.removeEventListener('keydown', handleKeydown))

function navigateTo(target: { agent?: Agent, taskId?: string }) {
  selectAgent(null)
  selectTask(null)
  nextTick(() => {
    if (target.agent)
      selectAgent(target.agent)
    if (target.taskId) {
      const t = tasks.value.find(t => t.id === target.taskId)
      if (t) {
        selectTask(t)
      }
      else {
        console.warn('[navigateTo] task not found locally:', target.taskId)
        showToast('Task not found — it may belong to a different machine.')
      }
    }
  })
}

interface QuotaInfo {
  periodStart: string | null
  periodEnd: string | null
  tokensUsed: number
  limit: number | null
}

const quota = ref<QuotaInfo | null>(null)

const quotaPct = computed<number | null>(() => {
  if (!quota.value?.limit)
    return null
  return Math.min(100, Math.round(quota.value.tokensUsed / quota.value.limit * 100))
})

async function fetchQuota() {
  const res = await fetch('/api/quota')
  if (res.ok)
    quota.value = await res.json() as QuotaInfo
}

onMounted(fetchQuota)
</script>

<template>
  <LoginPage v-if="loaded && showLogin" />
  <div v-else-if="loaded">
    <AppShell>
      <template #sidebar>
        <AppSidebar
          :agent-count="filteredAgents.length"
          :attention-count="combinedAttentionCount"
          :task-count="tasks.length"
          :live="live"
          :theme="theme"
          :can-install="canInstall"
          @open-sessions="showSessions = true"
          @toggle-theme="toggleTheme"
          @install="promptInstall"
        />
      </template>

      <template #topbar>
        <AppTopbar
          :active-view="activeView"
          :search-query="searchQuery"
          @open-settings="showSettings = true"
          @update:search-query="searchQuery = $event"
        >
          <template #cta>
            <button
              v-if="activeView === 'pipeline'"
              type="button"
              class="bg-accent text-white rounded-lg px-3 py-1.5 text-[13px] font-semibold hover:brightness-110 focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-card"
              @click="openNewTask"
            >
              + New Task
            </button>
            <button
              v-else-if="activeView === 'dashboard'"
              type="button"
              class="bg-accent text-white rounded-lg px-3 py-1.5 text-[13px] font-semibold hover:brightness-110 focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-card"
              @click="showSpawnDialog = true"
            >
              + New Agent
            </button>
          </template>
        </AppTopbar>
      </template>

      <div class="p-5 flex flex-col min-h-full">
        <div v-if="isLoading && activeView === 'dashboard'" class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3">
          <SkeletonCard v-for="n in 6" :key="n" />
        </div>
        <p v-else-if="error" class="text-center py-12 text-danger-text">
          Error: {{ error }}
        </p>

        <template v-else-if="activeView === 'dashboard'">
          <AgentTriageBand
            :agents="attentionAgents"
            :permission-items="permissionItems"
            :focused-session-id="focusedSessionId"
            @select="selectAgent"
            @toast="showToast"
            @remembered="autoApprovingStrip?.load()"
            @approve="(taskId, ids, remember) => approvePermission(taskId, ids, remember)"
            @deny="(taskId, ids) => denyPermission(taskId, ids)"
          />
          <AutoApprovingStrip ref="autoApprovingStrip" />
          <DashboardToolbar
            :layout="dashboardLayout"
            :spawner="dashboardSpawner"
            :project="dashboardProject"
            :sort-by="dashboardSort"
            :group-by="dashboardGroup"
            :project-options="projectOptions"
            :spawner-options="spawnerOptions"
            @update:layout="dashboardLayout = $event"
            @update:spawner="dashboardSpawner = $event"
            @update:project="dashboardProject = $event"
            @update:sort-by="dashboardSort = $event"
            @update:group-by="dashboardGroup = $event"
          />
          <template v-if="dashboardLayout === 'list'">
            <EmptyAgentState v-if="rosterAgents.length === 0" :search-query="searchQuery" />
            <AgentTable v-else :agents="rosterAgents" :groups="rosterGroups" @select="selectAgent" />
          </template>
          <template v-else>
            <EmptyAgentState v-if="rosterAgents.length === 0" :search-query="searchQuery" />
            <AgentCardGrid v-else :agents="rosterAgents" :groups="rosterGroups" @select="selectAgent" />
          </template>
          <ChannelScriptCallout />
        </template>

        <PipelineBoard
          v-else-if="activeView === 'pipeline'"
          :agents="agents"
          @select="selectTask"
          @open-chat="(t) => { activeConceptTask = t; showRefinementChat = true }"
          @navigate-agent="(sessionId) => { const a = agents.find(x => x.sessionId === sessionId); if (a) selectAgent(a) }"
        />
        <CostAnalyticsView v-else-if="activeView === 'cost'" />
        <EvalView v-else-if="activeView === 'eval'" />
        <WorkflowsView
          v-else-if="activeView === 'workflows'"
          @navigate="(sessionId) => { const a = agents.find(x => x.sessionId === sessionId); if (a) selectAgent(a) }"
        />
      </div>

      <template #statusbar>
        <AppStatusBar :cost-delta="costDelta" :today-cost-label="todayCostLabel" :quota-pct="quotaPct" />
      </template>
    </AppShell>

    <AgentModal :agent="selectedAgent" @close="selectAgent(null)" @navigate="(taskId: string) => navigateTo({ taskId })" />
    <TaskModal
      :task="selectedTask"
      @close="selectTask(null)"
      @navigate="(agent: Agent) => navigateTo({ agent })"
      @navigate-task="(taskId: string) => navigateTo({ taskId })"
      @open-chat="(t) => { selectTask(null); activeConceptTask = t; showRefinementChat = true }"
    />

    <!-- PWA update banner: shown when a new service worker is waiting to activate. -->
    <!-- F-UIUX-016: role="status" + aria-live rendered unconditionally so screen readers
         announce the content when it is inserted (ARIA live regions must exist in the DOM
         before content changes to be reliably announced). -->
    <div role="status" aria-live="polite" aria-atomic="true" class="pointer-events-none">
      <Transition name="toast">
        <div
          v-if="needsRefresh"
          class="pointer-events-auto fixed bottom-6 left-1/2 -translate-x-1/2 flex items-center gap-3 bg-slate-900 dark:bg-slate-800 border border-slate-700 text-slate-100 px-5 py-2.5 rounded-lg text-[13px] z-[2000] shadow-[0_4px_16px_rgba(0,0,0,0.4)]"
        >
          <span>A new version is available.</span>
          <button
            type="button"
            class="bg-blue-600 text-white border-none rounded-md px-3 py-1 text-[12px] font-semibold cursor-pointer hover:brightness-110"
            aria-label="A new version is available, reload to apply"
            @click="updateSW"
          >
            Reload
          </button>
        </div>
      </Transition>
    </div>
    <!-- F-UIUX-011: pointer-events enabled so hover pause works; timer resumes on mouseleave.
         Live region rendered unconditionally; content cleared to empty string when no toast. -->
    <div role="status" aria-live="polite" aria-atomic="true" class="pointer-events-none">
      <Transition name="toast">
        <div
          v-if="toastMessage"
          class="pointer-events-auto fixed bottom-6 left-1/2 -translate-x-1/2 bg-raised border border-line text-fg px-5 py-2.5 rounded-lg text-[13px] z-[2000] shadow-[0_4px_16px_rgba(0,0,0,0.4)]"
          @mouseenter="pauseToast"
          @mouseleave="resumeToast"
        >
          {{ toastMessage }}
        </div>
      </Transition>
    </div>
    <SpawnDialog :open="showSpawnDialog" @close="showSpawnDialog = false" />
    <RefinementChat
      :open="showRefinementChat"
      :task="activeConceptTask"
      @close="showRefinementChat = false; activeConceptTask = null"
      @confirmed="showRefinementChat = false; activeConceptTask = null"
    />
    <AppModal :open="showBacklogForm" width="560px" @close="showBacklogForm = false">
      <AppModalHeader title="New Task" @close="showBacklogForm = false" />
      <div class="flex-1 min-h-0 overflow-y-auto p-5">
        <BacklogForm @created-and-refine="onCreateTaskAndRefine" />
      </div>
    </AppModal>
    <SessionList :open="showSessions" :home-dir="homedir" @close="showSessions = false" />
    <ApiKeySettings :open="showSettings" @close="showSettings = false" />
    <EditGateModal />
    <SpotlightSearch
      @navigate-task="task => selectTask(task)"
      @navigate-agent="agent => selectAgent(agent)"
    />
  </div>
  <div v-else class="min-h-screen bg-app" />
</template>

<style>
.toast-enter-active, .toast-leave-active { transition: opacity 0.2s, transform 0.2s; }
.toast-enter-from, .toast-leave-to { opacity: 0; transform: translateX(-50%) translateY(8px); }
</style>
