<script setup lang="ts">
import type { Agent } from './types'
import { computed, nextTick, ref } from 'vue'
import AgentCardGrid from './components/AgentCardGrid.vue'
import AgentModal from './components/AgentModal.vue'
import AgentTable from './components/AgentTable.vue'
import ApiKeySettings from './components/ApiKeySettings.vue'
import BacklogForm from './components/BacklogForm.vue'
import CostTrend from './components/CostTrend.vue'
import PipelineBoard from './components/PipelineBoard.vue'
import ResourceBar from './components/ResourceBar.vue'
import SessionList from './components/SessionList.vue'
import SpawnDialog from './components/SpawnDialog.vue'
import TaskModal from './components/TaskModal.vue'
import { useAgents } from './composables/useAgents'
import { useTasks } from './composables/useTasks'
import { formatTokens, totalTokenCount } from './utils/format'

const { agents, costTrend, filteredAgents, selectedAgent, isLoading, error, searchQuery, viewMode, selectAgent } = useAgents()
const { tasks, selectedTask, selectTask } = useTasks()
const showSpawnDialog = ref(false)
const showBacklog = ref(false)
const showSessions = ref(false)
const showSettings = ref(false)
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
  <div class="app">
    <header class="app-header">
      <h1>Claude Agent Overview</h1>
      <span v-if="viewMode !== 'pipeline'" class="agent-count">{{ filteredAgents.length }} agent{{ filteredAgents.length !== 1 ? 's' : '' }}</span>
      <span v-else class="agent-count">{{ tasks.length }} task{{ tasks.length !== 1 ? 's' : '' }}</span>
      <span v-if="totalCost > 0" class="header-stat">${{ totalCost.toFixed(2) }}</span>
      <span v-if="totalTokens > 0" class="header-stat">{{ formatTokens(totalTokens) }} tokens</span>
      <input
        v-model="searchQuery"
        class="header-search"
        type="text"
        :placeholder="viewMode === 'pipeline' ? 'Search tasks...' : 'Search agents...'"
      >
      <div class="view-toggle">
        <button
          class="toggle-btn"
          :class="{ active: viewMode !== 'pipeline' }"
          title="Agent monitoring dashboard"
          @click="viewMode = viewMode === 'pipeline' ? 'cards' : viewMode"
        >
          Dashboard
        </button>
        <button
          class="toggle-btn"
          :class="{ active: viewMode === 'pipeline' }"
          title="Task pipeline kanban"
          @click="viewMode = 'pipeline'"
        >
          Kanban
        </button>
      </div>
      <button class="sessions-btn" @click="showSessions = true">
        Sessions
      </button>
      <button
        v-if="viewMode === 'pipeline'"
        class="spawn-btn"
        @click="showBacklog = true"
      >
        + New Task
      </button>
      <button v-else class="spawn-btn" @click="showSpawnDialog = true">
        + New Agent
      </button>
      <button class="settings-icon-btn" title="Settings" @click="showSettings = true; selectAgent(null); selectTask(null); showSessions = false; showSpawnDialog = false">
        ⚙
      </button>
    </header>
    <ResourceBar />
    <CostTrend :trend="costTrend" />
    <div v-if="scriptPath" class="script-banner">
      <span class="script-label">Channel script:</span>
      <code class="script-path" tabindex="0" role="button" :title="copied ? 'Copied!' : 'Click to copy'" @click="copyScript" @keydown.enter="copyScript" @keydown.space.prevent="copyScript">{{ scriptPath }}</code>
      <span v-if="copied" class="copied-hint">Copied!</span>
    </div>
    <div class="sub-toolbar" :class="{ 'sub-toolbar--hidden': showSettings || viewMode === 'pipeline' }">
      <button
        class="sub-toggle-btn"
        :class="{ active: viewMode === 'cards' }"
        title="Card view"
        @click="viewMode = 'cards'"
      >
        ⊞ Cards
      </button>
      <button
        class="sub-toggle-btn"
        :class="{ active: viewMode === 'list' }"
        title="List view"
        @click="viewMode = 'list'"
      >
        ≡ List
      </button>
    </div>
    <main>
      <p v-if="isLoading" class="loading">
        Loading agents...
      </p>
      <p v-else-if="error" class="error">
        Error: {{ error }}
      </p>
      <AgentTable
        v-else-if="viewMode === 'list'"
        :agents="filteredAgents"
        @select="selectAgent"
      />
      <PipelineBoard
        v-else-if="viewMode === 'pipeline'"
        @select="selectTask"
      />
      <AgentCardGrid
        v-else
        :agents="filteredAgents"
        @select="selectAgent"
      />
    </main>
    <AgentModal
      :agent="selectedAgent"
      @close="selectAgent(null)"
      @navigate="(taskId: string) => navigateTo({ taskId })"
    />
    <TaskModal
      :task="selectedTask"
      @close="selectTask(null)"
      @navigate="(agent: Agent) => navigateTo({ agent })"
    />
    <Transition name="toast">
      <div v-if="toastMessage" class="toast-notification">
        {{ toastMessage }}
      </div>
    </Transition>
    <SpawnDialog
      :open="showSpawnDialog"
      @close="showSpawnDialog = false"
    />
    <BacklogForm
      :open="showBacklog"
      @close="showBacklog = false"
    />
    <SessionList
      :open="showSessions"
      :home-dir="homeDir"
      @close="showSessions = false"
    />
    <ApiKeySettings
      :open="showSettings"
      @close="showSettings = false"
    />
  </div>
</template>
