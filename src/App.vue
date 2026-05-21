<script setup lang="ts">
import type { Agent, PipelineTask } from './types'
import { computed, defineAsyncComponent, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import AgentCardGrid from './components/AgentCardGrid.vue'
import AgentModal from './components/AgentModal.vue'
import AgentTable from './components/AgentTable.vue'
import EmptyAgentState from './components/EmptyAgentState.vue'
import ApiKeySettings from './components/ApiKeySettings.vue'
import AuditSettings from './components/AuditSettings.vue'
import AppModal from './components/ui/AppModal.vue'
import CostTrend from './components/CostTrend.vue'
import EditGateModal from './components/EditGateModal.vue'
import LoginPage from './components/LoginPage.vue'
import PipelineBoard from './components/PipelineBoard.vue'
import RefinementChat from './components/RefinementChat.vue'
import ResourceBar from './components/ResourceBar.vue'
import SessionList from './components/SessionList.vue'
import SpawnDialog from './components/SpawnDialog.vue'
import SpotlightSearch from './components/SpotlightSearch.vue'
import OfflineBadge from './components/OfflineBadge.vue'
import { useAgents } from './composables/useAgents'
import { useInstallPrompt } from './composables/useInstallPrompt'
import { usePWA } from './composables/usePWA'
import { useTasks } from './composables/useTasks'
import { useTheme } from './composables/useTheme'
import { useUser } from './composables/useUser'
import { formatCost, formatTokens, totalTokenCount } from './utils/format'

// Heavy modal loaded on demand — split into its own chunk (includes DependencyGraph + StageCostWaterfall).
const TaskModal = defineAsyncComponent(() => import('./components/TaskModal.vue'))
const ProjectSettings = defineAsyncComponent(() => import('./components/ProjectSettings.vue'))
const SpawnerSettings = defineAsyncComponent(() => import('./components/SpawnerSettings.vue'))

const { user, authEnabled, loaded, loadUser } = useUser()
const showLogin = computed(() => authEnabled.value && !user.value)
const { needsRefresh, updateSW } = usePWA()
const { canInstall, promptInstall } = useInstallPrompt()
const { toggleTheme } = useTheme()

// UX-08: Shift+D toggles dark/light mode globally
function handleKeydown(e: KeyboardEvent) {
  if (e.key === 'D' && e.shiftKey && !e.ctrlKey && !e.metaKey && !e.altKey) {
    const tag = (e.target as HTMLElement)?.tagName
    if (tag !== 'INPUT' && tag !== 'TEXTAREA' && tag !== 'SELECT' && !(e.target as HTMLElement).isContentEditable)
      toggleTheme()
  }
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
const { agents, costTrend, filteredAgents, selectedAgent, isLoading, error, searchQuery, viewMode, selectAgent, startStream: startAgents } = useAgents({ autoStart: false })
const { tasks, selectedTask, selectTask, startStream: startTasks } = useTasks({ autoStart: false })

// Start data streams only after auth is confirmed — avoids 401 flood while login page is shown
watch(loaded, (isLoaded) => {
  if (isLoaded && !showLogin.value) {
    startAgents()
    startTasks()
  }
}, { immediate: true })
const showSpawnDialog = ref(false)
const activeConceptTask = ref<PipelineTask | null>(null)
const showRefinementChat = ref(false)
const showSessions = ref(false)
const showSettings = ref(false)
const showAudit = ref(false)
const showProjects = ref(false)
const showSpawners = ref(false)

function openNewTask() {
  activeConceptTask.value = null
  showRefinementChat.value = true
}
const scriptPath = ref('')
const homeDir = ref('')
const copied = ref(false)

fetch('/api/config').then(r => r.json()).then((d) => {
  scriptPath.value = d.scriptPath
  homeDir.value = d.homedir
}).catch(() => {})

function copyScript() {
  navigator.clipboard.writeText(scriptPath.value)
  copied.value = true
  setTimeout(() => {
    copied.value = false
  }, 2000)
}

const toastMessage = ref<string | null>(null)
let toastTimer: ReturnType<typeof setTimeout> | null = null

function showToast(msg: string) {
  toastMessage.value = msg
  if (toastTimer)
    clearTimeout(toastTimer)
  toastTimer = setTimeout(() => {
    toastMessage.value = null
  }, 3500)
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

const totalCost = computed(() => agents.value.reduce((sum, a) => sum + a.costEstimate, 0))
const totalTokens = computed(() => agents.value.reduce((sum, a) => sum + totalTokenCount(a.tokenUsage), 0))

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

const quotaSeverity = computed(() =>
  quotaPct.value >= 90 ? 'critical' : quotaPct.value >= 75 ? 'warning' : 'normal',
)

const quotaPeriodEndLabel = computed(() => {
  if (!quota.value?.periodEnd)
    return 'Monthly quota'
  const end = new Date(quota.value.periodEnd)
  const now = new Date()
  const diffMs = end.getTime() - now.getTime()
  const diffDays = Math.ceil(diffMs / (1000 * 60 * 60 * 24))
  if (diffDays <= 0)
    return 'Resets soon'
  return `Resets in ${diffDays} day${diffDays === 1 ? '' : 's'}`
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
  <div v-else-if="loaded" class="h-screen flex flex-col bg-slate-50 dark:bg-slate-950 text-slate-900 dark:text-slate-100 font-sans">
    <!-- UX-33: Skip-to-content link for keyboard users -->
    <a
      href="#main-content"
      class="sr-only focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-[9999] focus:px-4 focus:py-2 focus:bg-blue-600 focus:text-white focus:rounded focus:text-sm focus:font-semibold"
    >Skip to main content</a>
    <header class="shrink-0 flex flex-wrap items-center gap-3 gap-y-2 px-6 py-4 border-b border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900">
      <h1 class="text-[18px] font-semibold text-slate-900 dark:text-slate-100">
        Claude Agent Overview
      </h1>
      <span class="text-xs text-slate-400 dark:text-slate-600 bg-slate-100 dark:bg-slate-800 px-2.5 py-0.5 rounded-full">
        <template v-if="viewMode !== 'pipeline'">{{ filteredAgents.length }} agent{{ filteredAgents.length !== 1 ? 's' : '' }}</template>
        <template v-else>{{ tasks.length }} task{{ tasks.length !== 1 ? 's' : '' }}</template>
      </span>
      <span v-if="totalCost > 0" class="text-xs text-green-600 dark:text-green-400 bg-slate-100 dark:bg-slate-800 px-2.5 py-0.5 rounded-full font-mono">{{ formatCost(totalCost) }}</span>
      <span v-if="totalTokens > 0" class="text-xs text-green-600 dark:text-green-400 bg-slate-100 dark:bg-slate-800 px-2.5 py-0.5 rounded-full font-mono">{{ formatTokens(totalTokens) }} tokens</span>
      <div v-if="quota && quota.limit" class="flex items-center gap-1.5" :title="`${quota.tokensUsed.toLocaleString()} / ${quota.limit.toLocaleString()} tokens — ${quotaPeriodEndLabel}`">
        <span class="text-[10px] text-slate-400">Quota</span>
        <div
          class="w-20 h-1.5 bg-slate-200 dark:bg-slate-700 rounded-full overflow-hidden"
          role="progressbar"
          :aria-valuenow="quotaPct"
          aria-valuemin="0"
          aria-valuemax="100"
          :aria-label="`Quota ${quotaSeverity}: ${quotaPct}% used — ${quotaPeriodEndLabel}`"
        >
          <div
            class="h-full rounded-full transition-all"
            :class="{
              'bg-red-500': quotaPct >= 90,
              'bg-yellow-500': quotaPct >= 75 && quotaPct < 90,
              'bg-green-500': quotaPct < 75,
            }"
            :style="{ width: `${quotaPct}%` }"
          />
        </div>
        <span class="text-[10px] text-slate-400">{{ quotaPct }}%</span>
      </div>
      <input
        v-model="searchQuery"
        type="text"
        :placeholder="viewMode === 'pipeline' ? 'Search tasks...' : 'Search agents...'"
        class="ml-auto bg-slate-100 dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-md px-3 py-1.5 text-[13px] text-slate-900 dark:text-slate-100 placeholder:text-slate-400 dark:placeholder:text-slate-600 w-[200px] focus:outline-none focus:border-blue-500 focus:w-[260px] transition-[width] duration-200"
      >
      <div class="flex bg-slate-100 dark:bg-slate-800 rounded-md overflow-hidden">
        <button
          type="button"
          class="px-3 py-2 min-h-[44px] text-[13px] font-sans border-none cursor-pointer transition-all"
          :class="viewMode !== 'pipeline' ? 'bg-blue-600 text-white' : 'bg-transparent text-slate-400 dark:text-slate-600 hover:text-slate-700 dark:hover:text-slate-300'"
          title="Agent monitoring dashboard"
          @click="viewMode = viewMode === 'pipeline' ? 'cards' : viewMode"
        >
          Dashboard
        </button>
        <button
          type="button"
          class="px-3 py-2 min-h-[44px] text-[13px] font-sans border-none cursor-pointer transition-all"
          :class="viewMode === 'pipeline' ? 'bg-blue-600 text-white' : 'bg-transparent text-slate-400 dark:text-slate-600 hover:text-slate-700 dark:hover:text-slate-300'"
          title="Task pipeline kanban"
          @click="viewMode = 'pipeline'"
        >
          Kanban
        </button>
      </div>
      <button
        type="button"
        class="bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 border-none rounded-md px-3.5 py-2 min-h-[44px] text-[13px] font-semibold cursor-pointer font-sans whitespace-nowrap hover:text-slate-700 dark:hover:text-slate-200 hover:brightness-110"
        @click="showSessions = true"
      >
        Sessions
      </button>
      <button
        v-if="viewMode === 'pipeline'"
        type="button"
        class="bg-green-600 text-white border-none rounded-md px-3.5 py-2 min-h-[44px] text-[13px] font-semibold cursor-pointer font-sans whitespace-nowrap hover:brightness-110"
        @click="openNewTask"
      >
        + New Task
      </button>
      <button
        v-else
        type="button"
        class="bg-green-600 text-white border-none rounded-md px-3.5 py-2 min-h-[44px] text-[13px] font-semibold cursor-pointer font-sans whitespace-nowrap hover:brightness-110"
        @click="showSpawnDialog = true"
      >
        + New Agent
      </button>
      <button
        type="button"
        class="bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 border-none rounded-md px-3.5 py-2 min-h-[44px] text-[13px] font-semibold cursor-pointer font-sans whitespace-nowrap hover:text-slate-700 dark:hover:text-slate-200 hover:brightness-110"
        title="Audit Log"
        @click="showAudit = true"
      >
        Audit
      </button>
      <button
        type="button"
        class="bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 border-none rounded-md px-3.5 py-2 min-h-[44px] text-[13px] font-semibold cursor-pointer font-sans whitespace-nowrap hover:text-slate-700 dark:hover:text-slate-200 hover:brightness-110"
        title="Projects"
        @click="showProjects = true"
      >
        Projects
      </button>
      <button
        type="button"
        class="bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 border-none rounded-md px-3.5 py-2 min-h-[44px] text-[13px] font-semibold cursor-pointer font-sans whitespace-nowrap hover:text-slate-700 dark:hover:text-slate-200 hover:brightness-110"
        title="Spawners"
        @click="showSpawners = true"
      >
        Spawners
      </button>
      <OfflineBadge />
      <button
        type="button"
        class="bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-600 border-none rounded-md min-w-[44px] min-h-[44px] px-2.5 py-2 text-base cursor-pointer leading-none hover:text-slate-700 dark:hover:text-slate-300 hover:brightness-110"
        title="Settings"
        @click="showSettings = true"
      >
        ⚙
      </button>
      <button
        v-if="canInstall"
        type="button"
        class="bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 border-none rounded-md px-3.5 py-2 min-h-[44px] text-[13px] font-semibold cursor-pointer font-sans whitespace-nowrap hover:text-slate-700 dark:hover:text-slate-200 hover:brightness-110"
        title="Install Agent Dashboard as a PWA"
        @click="promptInstall"
      >
        Install app
      </button>
    </header>

    <div class="shrink-0"><ResourceBar /></div>
    <div class="shrink-0"><CostTrend :trend="costTrend" /></div>

    <div v-if="scriptPath" class="shrink-0 flex items-center gap-2 px-6 py-1.5 bg-white dark:bg-slate-900 border-b border-slate-200 dark:border-slate-700 text-xs">
      <span class="text-slate-400 dark:text-slate-600 whitespace-nowrap">Channel script:</span>
      <code
        class="font-mono text-[11px] text-slate-500 dark:text-slate-400 bg-slate-50 dark:bg-slate-950 px-2 py-0.5 rounded cursor-pointer select-all transition-colors hover:text-green-600 dark:hover:text-green-400 focus-visible:outline-2 focus-visible:outline-blue-500"
        tabindex="0"
        role="button"
        :title="copied ? 'Copied!' : 'Click to copy'"
        @click="copyScript"
        @keydown.enter="copyScript"
        @keydown.space.prevent="copyScript"
      >{{ scriptPath }}</code>
      <span v-if="copied" class="text-green-600 dark:text-green-400 text-[11px]">Copied!</span>
    </div>

    <div
      v-show="viewMode !== 'pipeline'"
      class="shrink-0 flex items-center gap-1 px-6 py-2 border-b border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900"
    >
      <button
        type="button"
        class="border-none px-2.5 py-1 text-xs cursor-pointer rounded-md font-sans transition-all"
        :class="viewMode === 'cards' ? 'bg-slate-100 dark:bg-slate-800 text-slate-700 dark:text-slate-300' : 'bg-transparent text-slate-400 dark:text-slate-600 hover:text-slate-500 dark:hover:text-slate-400'"
        title="Card view"
        @click="viewMode = 'cards'"
      >
        ⊞ Cards
      </button>
      <button
        type="button"
        class="border-none px-2.5 py-1 text-xs cursor-pointer rounded-md font-sans transition-all"
        :class="viewMode === 'list' ? 'bg-slate-100 dark:bg-slate-800 text-slate-700 dark:text-slate-300' : 'bg-transparent text-slate-400 dark:text-slate-600 hover:text-slate-500 dark:hover:text-slate-400'"
        title="List view"
        @click="viewMode = 'list'"
      >
        ≡ List
      </button>
    </div>
    <main id="main-content" class="p-6 flex-1 min-h-0 overflow-y-auto">
      <p v-if="isLoading" class="text-center py-12 text-slate-400 dark:text-slate-600">
        Loading agents...
      </p>
      <p v-else-if="error" class="text-center py-12 text-red-600 dark:text-red-400">
        Error: {{ error }}
      </p>
      <template v-else-if="viewMode === 'list'">
        <EmptyAgentState v-if="filteredAgents.length === 0" :search-query="searchQuery" />
        <AgentTable v-else :agents="filteredAgents" @select="selectAgent" />
      </template>
      <PipelineBoard
        v-else-if="viewMode === 'pipeline'"
        @select="selectTask"
        @open-chat="(t) => { activeConceptTask = t; showRefinementChat = true }"
      />
      <template v-else>
        <EmptyAgentState v-if="filteredAgents.length === 0" :search-query="searchQuery" />
        <AgentCardGrid v-else :agents="filteredAgents" @select="selectAgent" />
      </template>
    </main>

    <AgentModal :agent="selectedAgent" @close="selectAgent(null)" @navigate="(taskId: string) => navigateTo({ taskId })" />
    <TaskModal
      :task="selectedTask"
      @close="selectTask(null)"
      @navigate="(agent: Agent) => navigateTo({ agent })"
      @navigate-task="(taskId: string) => navigateTo({ taskId })"
      @open-chat="(t) => { selectTask(null); activeConceptTask = t; showRefinementChat = true }"
    />

    <!-- PWA update banner: shown when a new service worker is waiting to activate. -->
    <Transition name="toast">
      <div
        v-if="needsRefresh"
        class="fixed bottom-6 left-1/2 -translate-x-1/2 flex items-center gap-3 bg-slate-900 dark:bg-slate-800 border border-slate-700 text-slate-100 px-5 py-2.5 rounded-lg text-[13px] z-[2000] shadow-[0_4px_16px_rgba(0,0,0,0.4)]"
      >
        <span>A new version is available.</span>
        <button
          type="button"
          class="bg-blue-600 text-white border-none rounded-md px-3 py-1 text-[12px] font-semibold cursor-pointer hover:brightness-110"
          @click="updateSW"
        >
          Reload
        </button>
      </div>
    </Transition>
    <Transition name="toast">
      <div
        v-if="toastMessage"
        role="status"
        aria-live="polite"
        class="fixed bottom-6 left-1/2 -translate-x-1/2 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 text-slate-900 dark:text-slate-100 px-5 py-2.5 rounded-lg text-[13px] z-[2000] shadow-[0_4px_16px_rgba(0,0,0,0.4)] pointer-events-none"
      >
        {{ toastMessage }}
      </div>
    </Transition>
    <SpawnDialog :open="showSpawnDialog" @close="showSpawnDialog = false" />
    <RefinementChat
      :open="showRefinementChat"
      :task="activeConceptTask"
      @task-created="activeConceptTask = $event"
      @close="showRefinementChat = false; activeConceptTask = null"
      @confirmed="showRefinementChat = false; activeConceptTask = null"
    />
    <SessionList :open="showSessions" :home-dir="homeDir" @close="showSessions = false" />
    <ApiKeySettings :open="showSettings" @close="showSettings = false" />
    <AuditSettings :open="showAudit" @close="showAudit = false" />
    <AppModal :open="showProjects" @close="showProjects = false">
      <div class="bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 shadow-[0_8px_40px_rgba(0,0,0,0.5)] w-full max-w-5xl max-h-[85vh] flex flex-col overflow-hidden">
        <div class="flex justify-end px-5 pt-4 flex-shrink-0">
          <button
            type="button"
            class="bg-transparent border-none text-slate-400 dark:text-slate-600 text-2xl cursor-pointer px-1 leading-none hover:text-slate-900 dark:hover:text-slate-100"
            aria-label="Close"
            @click="showProjects = false"
          >
            &times;
          </button>
        </div>
        <div class="flex-1 overflow-y-auto px-5 pb-5">
          <ProjectSettings v-if="showProjects" />
        </div>
      </div>
    </AppModal>
    <AppModal :open="showSpawners" @close="showSpawners = false">
      <div class="bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 shadow-[0_8px_40px_rgba(0,0,0,0.5)] w-full max-w-5xl max-h-[85vh] flex flex-col overflow-hidden">
        <div class="flex justify-end px-5 pt-4 flex-shrink-0">
          <button
            type="button"
            class="bg-transparent border-none text-slate-400 dark:text-slate-600 text-2xl cursor-pointer px-1 leading-none hover:text-slate-900 dark:hover:text-slate-100"
            aria-label="Close"
            @click="showSpawners = false"
          >
            &times;
          </button>
        </div>
        <div class="flex-1 overflow-y-auto px-5 pb-5">
          <SpawnerSettings v-if="showSpawners" />
        </div>
      </div>
    </AppModal>
    <EditGateModal />
    <SpotlightSearch
      @navigate-task="task => selectTask(task)"
      @navigate-agent="agent => selectAgent(agent)"
    />
  </div>
  <div v-else class="min-h-screen bg-slate-50 dark:bg-slate-950" />
</template>

<style>
.toast-enter-active, .toast-leave-active { transition: opacity 0.2s, transform 0.2s; }
.toast-enter-from, .toast-leave-to { opacity: 0; transform: translateX(-50%) translateY(8px); }
</style>
