<script setup lang="ts">
import type { Agent, PipelineTask } from './types'
import { computed, defineAsyncComponent, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import AgentCardGrid from './components/AgentCardGrid.vue'
import AgentModal from './components/AgentModal.vue'
import AgentTable from './components/AgentTable.vue'
import ApiKeySettings from './components/ApiKeySettings.vue'
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
import { useAgents } from './composables/useAgents'
import { useInstallPrompt } from './composables/useInstallPrompt'
import { usePWA } from './composables/usePWA'
import { useSidebar } from './composables/useSidebar'
import { useTasks } from './composables/useTasks'
import { useTodayCost } from './composables/useTodayCost'
import { useTheme } from './composables/useTheme'
import { useUser } from './composables/useUser'
import { useViewState } from './composables/useViewState'
import { formatCost, formatTokens, totalTokenCount } from './utils/format'

// F-PERF-019: top-level heavy views loaded on demand — each becomes its own chunk
const CostAnalyticsView = defineAsyncComponent(() => import('./components/CostAnalyticsView.vue'))
const PipelineBoard = defineAsyncComponent(() => import('./components/PipelineBoard.vue'))
const WorkflowsView = defineAsyncComponent(() => import('./components/WorkflowsView.vue'))
// Heavy modal loaded on demand — split into its own chunk (includes DependencyGraph + StageCostWaterfall).
const TaskModal = defineAsyncComponent(() => import('./components/TaskModal.vue'))
// Modal/panel components that drag in marked + dompurify (RefinementChat) and diff (EditGateModal) —
// load on demand so those libs stay out of the first-load entry chunk.
const RefinementChat = defineAsyncComponent(() => import('./components/RefinementChat.vue'))
const EditGateModal = defineAsyncComponent(() => import('./components/EditGateModal.vue'))

const { user, authEnabled, loaded, loadUser } = useUser()
const showLogin = computed(() => authEnabled.value && !user.value)
const { needsRefresh, updateSW } = usePWA()
const { canInstall, promptInstall } = useInstallPrompt()
const { theme, toggleTheme } = useTheme()

const { activeView, dashboardLayout } = useViewState()
const { handleShortcut: handleSidebarShortcut } = useSidebar()

// F-UIUX-011: 5 s default duration; hover pause/resume keeps toast visible while pointer rests on it
const TOAST_DURATION_MS = 5000
const toastMessage = ref<string | null>(null)
let toastTimer: ReturnType<typeof setTimeout> | null = null
let toastPaused = false

// UX-08: Shift+D toggles dark/light mode globally; Cmd/Ctrl+B toggles sidebar pin
function handleKeydown(e: KeyboardEvent) {
  if (e.key === 'D' && e.shiftKey && !e.ctrlKey && !e.metaKey && !e.altKey) {
    const tag = (e.target as HTMLElement)?.tagName
    if (tag !== 'INPUT' && tag !== 'TEXTAREA' && tag !== 'SELECT' && !(e.target as HTMLElement).isContentEditable)
      toggleTheme()
  }
  handleSidebarShortcut(e)
}

onMounted(() => {
  loadUser()
  window.addEventListener('keydown', handleKeydown)
})

onUnmounted(() => {
  window.removeEventListener('keydown', handleKeydown)
  if (toastTimer)
    clearTimeout(toastTimer)
})

const { agents, costTrend, filteredAgents, selectedAgent, isLoading, error, searchQuery, hideNonClaude, selectAgent, startStream: startAgents } = useAgents({ autoStart: false })
const { tasks, selectedTask, selectTask, startStream: startTasks } = useTasks({ autoStart: false })
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
const totalCostLabel = computed(() => formatCost(totalCost.value))
const totalTokensLabel = computed(() => formatTokens(totalTokens.value))

const todayCostLabel = computed(() => (todayUsd.value === null ? '—' : formatCost(todayUsd.value)))

const showSpawnDialog = ref(false)
const activeConceptTask = ref<PipelineTask | null>(null)
const showRefinementChat = ref(false)
const showBacklogForm = ref(false)
const showSessions = ref(false)
const showSettings = ref(false)
const homeDir = ref('')

fetch('/api/config').then(r => r.json()).then((d) => {
  homeDir.value = d.homedir
}).catch(() => {})

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

const quotaPct = computed(() => {
  if (!quota.value?.limit)
    return 0
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
          :task-count="tasks.length"
          :total-cost-label="totalCostLabel"
          :total-tokens-label="totalTokensLabel"
          :today-cost-label="todayCostLabel"
          :quota-pct="quotaPct"
          :theme="theme"
          :can-install="canInstall"
          @open-sessions="showSessions = true"
          @open-settings="showSettings = true"
          @toggle-theme="toggleTheme"
          @install="promptInstall"
        />
      </template>

      <template #topbar>
        <AppTopbar
          :active-view="activeView"
          :search-query="searchQuery"
          :live="live"
          @update:search-query="searchQuery = $event"
        >
          <template #cta>
            <button
              v-if="activeView === 'pipeline'"
              type="button"
              class="bg-accent text-white rounded-lg px-3 py-1.5 text-[13px] font-semibold hover:brightness-110 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-card"
              @click="openNewTask"
            >
              + New Task
            </button>
            <button
              v-else-if="activeView === 'dashboard'"
              type="button"
              class="bg-accent text-white rounded-lg px-3 py-1.5 text-[13px] font-semibold hover:brightness-110 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-card"
              @click="showSpawnDialog = true"
            >
              + New Agent
            </button>
          </template>
        </AppTopbar>
      </template>

      <div class="p-5 flex flex-col min-h-full">
        <DashboardToolbar
          v-if="activeView === 'dashboard'"
          :layout="dashboardLayout"
          :hide-non-claude="hideNonClaude"
          @update:layout="dashboardLayout = $event"
          @update:hide-non-claude="hideNonClaude = $event"
        />

        <div v-if="isLoading && activeView === 'dashboard'" class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3">
          <SkeletonCard v-for="n in 6" :key="n" />
        </div>
        <p v-else-if="error" class="text-center py-12 text-red-600 dark:text-red-400">
          Error: {{ error }}
        </p>

        <template v-else-if="activeView === 'dashboard'">
          <template v-if="dashboardLayout === 'list'">
            <EmptyAgentState v-if="filteredAgents.length === 0" :search-query="searchQuery" />
            <AgentTable v-else :agents="filteredAgents" @select="selectAgent" />
          </template>
          <template v-else>
            <EmptyAgentState v-if="filteredAgents.length === 0" :search-query="searchQuery" />
            <AgentCardGrid v-else :agents="filteredAgents" @select="selectAgent" />
          </template>
          <ChannelScriptCallout />
        </template>

        <PipelineBoard
          v-else-if="activeView === 'pipeline'"
          @select="selectTask"
          @open-chat="(t) => { activeConceptTask = t; showRefinementChat = true }"
        />
        <CostAnalyticsView v-else-if="activeView === 'cost'" />
        <WorkflowsView
          v-else-if="activeView === 'workflows'"
          @navigate="(sessionId) => { const a = agents.find(x => x.sessionId === sessionId); if (a) selectAgent(a) }"
        />
      </div>

      <template #statusbar>
        <AppStatusBar :cost-delta="costDelta" :today-cost-label="todayCostLabel" />
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
    <AppModal :open="showBacklogForm" @close="showBacklogForm = false">
      <header class="flex items-center justify-between px-5 py-4 border-b border-line shrink-0">
        <h2 class="text-base font-semibold text-fg">
          New Task
        </h2>
        <button
          type="button"
          class="bg-transparent border-none text-fg-mute text-base cursor-pointer px-2 py-1 rounded hover:bg-slate-200 dark:hover:bg-slate-700 hover:text-fg"
          data-testid="close-backlog-form"
          @click="showBacklogForm = false"
        >
          ✕
        </button>
      </header>
      <div class="flex-1 min-h-0 overflow-y-auto p-5">
        <BacklogForm @created-and-refine="onCreateTaskAndRefine" />
      </div>
    </AppModal>
    <SessionList :open="showSessions" :home-dir="homeDir" @close="showSessions = false" />
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
