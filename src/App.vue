<script setup lang="ts">
import type { Agent, PipelineTask } from './types'
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import AgentCardGrid from './components/AgentCardGrid.vue'
import AgentModal from './components/AgentModal.vue'
import AgentTable from './components/AgentTable.vue'
import ApiKeySettings from './components/ApiKeySettings.vue'
import CostTrend from './components/CostTrend.vue'
import LoginPage from './components/LoginPage.vue'
import PipelineBoard from './components/PipelineBoard.vue'
import RefinementChat from './components/RefinementChat.vue'
import ResourceBar from './components/ResourceBar.vue'
import SessionList from './components/SessionList.vue'
import SpawnDialog from './components/SpawnDialog.vue'
import TaskModal from './components/TaskModal.vue'
import { useAgents } from './composables/useAgents'
import { createTask, useTasks } from './composables/useTasks'
import { useUser } from './composables/useUser'
import { formatTokens, totalTokenCount } from './utils/format'

const { user, authEnabled, loaded, loadUser } = useUser()
const showLogin = computed(() => authEnabled.value && !user.value)

onMounted(() => {
  loadUser()
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
const activeKonzeptTask = ref<PipelineTask | null>(null)
const showSessions = ref(false)
const showSettings = ref(false)

async function openNewTask() {
  try {
    const task = await createTask({
      slug: `konzept-${Date.now()}`,
      title: 'New Task',
      cwd: '/',
      stage: 'konzept',
    })
    if (task)
      activeKonzeptTask.value = task
  }
  catch (err) {
    showToast(`Failed to create task: ${(err as Error).message}`)
  }
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
</script>

<template>
  <LoginPage v-if="loaded && showLogin" />
  <div v-else-if="loaded" class="min-h-screen bg-slate-50 dark:bg-slate-950 text-slate-900 dark:text-slate-100 font-sans">
    <header class="flex items-center gap-3 px-6 py-4 border-b border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900">
      <h1 class="text-[18px] font-semibold text-slate-900 dark:text-slate-100">
        Claude Agent Overview
      </h1>
      <span class="text-xs text-slate-400 dark:text-slate-600 bg-slate-100 dark:bg-slate-800 px-2.5 py-0.5 rounded-full">
        <template v-if="viewMode !== 'pipeline'">{{ filteredAgents.length }} agent{{ filteredAgents.length !== 1 ? 's' : '' }}</template>
        <template v-else>{{ tasks.length }} task{{ tasks.length !== 1 ? 's' : '' }}</template>
      </span>
      <span v-if="totalCost > 0" class="text-xs text-green-600 dark:text-green-400 bg-slate-100 dark:bg-slate-800 px-2.5 py-0.5 rounded-full font-mono">${{ totalCost.toFixed(2) }}</span>
      <span v-if="totalTokens > 0" class="text-xs text-green-600 dark:text-green-400 bg-slate-100 dark:bg-slate-800 px-2.5 py-0.5 rounded-full font-mono">{{ formatTokens(totalTokens) }} tokens</span>
      <input
        v-model="searchQuery"
        type="text"
        :placeholder="viewMode === 'pipeline' ? 'Search tasks...' : 'Search agents...'"
        class="ml-auto bg-slate-100 dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-md px-3 py-1.5 text-[13px] text-slate-900 dark:text-slate-100 placeholder:text-slate-400 dark:placeholder:text-slate-600 w-[200px] focus:outline-none focus:border-blue-500 focus:w-[260px] transition-[width] duration-200"
      >
      <div class="flex bg-slate-100 dark:bg-slate-800 rounded-md overflow-hidden">
        <button
          type="button"
          class="px-3 py-1.5 text-[13px] font-sans border-none cursor-pointer transition-all"
          :class="viewMode !== 'pipeline' ? 'bg-blue-600 text-white' : 'bg-transparent text-slate-400 dark:text-slate-600 hover:text-slate-700 dark:hover:text-slate-300'"
          title="Agent monitoring dashboard"
          @click="viewMode = viewMode === 'pipeline' ? 'cards' : viewMode"
        >
          Dashboard
        </button>
        <button
          type="button"
          class="px-3 py-1.5 text-[13px] font-sans border-none cursor-pointer transition-all"
          :class="viewMode === 'pipeline' ? 'bg-blue-600 text-white' : 'bg-transparent text-slate-400 dark:text-slate-600 hover:text-slate-700 dark:hover:text-slate-300'"
          title="Task pipeline kanban"
          @click="viewMode = 'pipeline'"
        >
          Kanban
        </button>
      </div>
      <button
        type="button"
        class="bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 border-none rounded-md px-3.5 py-1.5 text-[13px] font-semibold cursor-pointer font-sans whitespace-nowrap hover:text-slate-700 dark:hover:text-slate-200 hover:brightness-110"
        @click="showSessions = true"
      >
        Sessions
      </button>
      <button
        v-if="viewMode === 'pipeline'"
        type="button"
        class="bg-green-600 text-white border-none rounded-md px-3.5 py-1.5 text-[13px] font-semibold cursor-pointer font-sans whitespace-nowrap hover:brightness-110"
        @click="openNewTask"
      >
        + New Task
      </button>
      <button
        v-else
        type="button"
        class="bg-green-600 text-white border-none rounded-md px-3.5 py-1.5 text-[13px] font-semibold cursor-pointer font-sans whitespace-nowrap hover:brightness-110"
        @click="showSpawnDialog = true"
      >
        + New Agent
      </button>
      <button
        type="button"
        class="bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-600 border-none rounded-md px-2.5 py-1.5 text-base cursor-pointer leading-none hover:text-slate-700 dark:hover:text-slate-300 hover:brightness-110"
        title="Settings"
        @click="showSettings = true"
      >
        ⚙
      </button>
    </header>

    <ResourceBar />
    <CostTrend :trend="costTrend" />

    <div v-if="scriptPath" class="flex items-center gap-2 px-6 py-1.5 bg-white dark:bg-slate-900 border-b border-slate-200 dark:border-slate-700 text-xs">
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
      class="flex items-center gap-1 px-6 py-2 border-b border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900"
      :class="{ 'invisible pointer-events-none': viewMode === 'pipeline' }"
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
    <main class="p-6">
      <p v-if="isLoading" class="text-center py-12 text-slate-400 dark:text-slate-600">
        Loading agents...
      </p>
      <p v-else-if="error" class="text-center py-12 text-red-600 dark:text-red-400">
        Error: {{ error }}
      </p>
      <AgentTable v-else-if="viewMode === 'list'" :agents="filteredAgents" @select="selectAgent" />
      <PipelineBoard
        v-else-if="viewMode === 'pipeline'"
        @select="selectTask"
        @open-chat="activeKonzeptTask = $event"
      />
      <AgentCardGrid v-else :agents="filteredAgents" @select="selectAgent" />
    </main>

    <AgentModal :agent="selectedAgent" @close="selectAgent(null)" @navigate="(taskId: string) => navigateTo({ taskId })" />
    <TaskModal
      :task="selectedTask"
      @close="selectTask(null)"
      @navigate="(agent: Agent) => navigateTo({ agent })"
      @open-chat="(t) => { selectTask(null); activeKonzeptTask = t }"
    />


    <Transition name="toast">
      <div
        v-if="toastMessage"
        class="fixed bottom-6 left-1/2 -translate-x-1/2 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 text-slate-900 dark:text-slate-100 px-5 py-2.5 rounded-lg text-[13px] z-[2000] shadow-[0_4px_16px_rgba(0,0,0,0.4)] pointer-events-none"
      >
        {{ toastMessage }}
      </div>
    </Transition>
    <SpawnDialog :open="showSpawnDialog" @close="showSpawnDialog = false" />
    <RefinementChat
      :open="activeKonzeptTask !== null"
      :task="activeKonzeptTask"
      @close="activeKonzeptTask = null"
      @confirmed="activeKonzeptTask = null"
    />
    <SessionList :open="showSessions" :home-dir="homeDir" @close="showSessions = false" />
    <ApiKeySettings :open="showSettings" @close="showSettings = false" />
  </div>
  <div v-else class="min-h-screen bg-slate-50 dark:bg-slate-950" />
</template>

<style>
.toast-enter-active, .toast-leave-active { transition: opacity 0.2s, transform 0.2s; }
.toast-enter-from, .toast-leave-to { opacity: 0; transform: translateX(-50%) translateY(8px); }
</style>
