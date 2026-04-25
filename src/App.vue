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

<style>
:root {
  --bg-primary: #0f172a;
  --bg-secondary: #1e293b;
  --bg-tertiary: #334155;
  --text-primary: #e2e8f0;
  --text-secondary: #94a3b8;
  --text-muted: #64748b;
  --accent-green: #4ade80;
  --accent-yellow: #facc15;
  --accent-gray: #64748b;
  --accent-blue: #3b82f6;
  --accent-red: #f87171;
  --border: #1e293b;
  --font-mono: 'SF Mono', 'Fira Code', 'Cascadia Code', 'Menlo', monospace;
}

[data-theme="light"] {
  --bg-primary: #f8fafc;
  --bg-secondary: #e2e8f0;
  --bg-tertiary: #cbd5e1;
  --text-primary: #0f172a;
  --text-secondary: #475569;
  --text-muted: #94a3b8;
  --accent-green: #16a34a;
  --accent-yellow: #ca8a04;
  --accent-gray: #94a3b8;
  --accent-blue: #2563eb;
  --accent-red: #dc2626;
  --border: #cbd5e1;
}

* {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
  scrollbar-width: thin;
  scrollbar-color: var(--bg-tertiary) transparent;
}

::-webkit-scrollbar {
  width: 6px;
  height: 6px;
}

::-webkit-scrollbar-track {
  background: transparent;
}

::-webkit-scrollbar-thumb {
  background: var(--bg-tertiary);
  border-radius: 3px;
}

::-webkit-scrollbar-thumb:hover {
  background: var(--text-muted);
}

body {
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
  background: var(--bg-primary);
  color: var(--text-primary);
  min-height: 100vh;
}

.app-header {
  padding: 16px 24px;
  border-bottom: 1px solid var(--border);
  display: flex;
  align-items: center;
  gap: 12px;
}

.app-header h1 {
  font-size: 18px;
  font-weight: 600;
}

.agent-count {
  font-size: 12px;
  color: var(--text-muted);
  background: var(--bg-secondary);
  padding: 2px 10px;
  border-radius: 12px;
}

.header-stat {
  font-size: 12px;
  color: var(--accent-green);
  background: var(--bg-secondary);
  padding: 2px 10px;
  border-radius: 12px;
  font-family: var(--font-mono);
}

.theme-btn {
  background: var(--bg-tertiary);
  color: var(--text-primary);
  border: none;
  border-radius: 6px;
  padding: 6px 10px;
  font-size: 16px;
  cursor: pointer;
  line-height: 1;
  transition: filter 0.15s;
}

.theme-btn:hover {
  filter: brightness(1.2);
}

.sessions-btn {
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  border: none;
  border-radius: 6px;
  padding: 6px 14px;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  font-family: inherit;
  white-space: nowrap;
}

.sessions-btn:hover {
  color: var(--text-primary);
  filter: brightness(1.15);
}

.settings-icon-btn {
  background: var(--bg-tertiary);
  color: var(--text-muted);
  border: none;
  border-radius: 6px;
  padding: 6px 10px;
  font-size: 16px;
  cursor: pointer;
  line-height: 1;
  transition: color 0.15s, filter 0.15s;
}
.settings-icon-btn:hover {
  color: var(--text-primary);
  filter: brightness(1.15);
}

.spawn-btn {
  margin-left: 0;
  background: var(--accent-green);
  color: var(--bg-primary);
  border: none;
  border-radius: 6px;
  padding: 6px 14px;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  font-family: inherit;
  white-space: nowrap;
}

.spawn-btn:hover {
  filter: brightness(1.1);
}

.header-search {
  margin-left: auto;
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 6px 12px;
  font-size: 13px;
  color: var(--text-primary);
  font-family: inherit;
  width: 200px;
  transition: border-color 0.15s, width 0.2s;
}
.header-search::placeholder { color: var(--text-muted); }
.header-search:focus {
  outline: none;
  border-color: var(--accent-blue);
  width: 260px;
}

.view-toggle {
  display: flex;
  background: var(--bg-tertiary);
  border-radius: 6px;
  overflow: hidden;
}
.toggle-btn {
  background: none;
  border: none;
  color: var(--text-muted);
  padding: 6px 12px;
  font-size: 13px;
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
  font-family: inherit;
  white-space: nowrap;
}
.toggle-btn.active {
  background: var(--accent-blue);
  color: white;
}
.toggle-btn:not(.active):hover {
  color: var(--text-primary);
}

.sub-toolbar {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 8px 24px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-secondary);
}
.sub-toggle-btn {
  background: none;
  border: none;
  color: var(--text-muted);
  padding: 4px 10px;
  font-size: 12px;
  cursor: pointer;
  border-radius: 4px;
  font-family: inherit;
  transition: background 0.15s, color 0.15s;
}
.sub-toolbar--hidden {
  visibility: hidden;
  pointer-events: none;
}
.sub-toggle-btn.active {
  background: var(--bg-tertiary);
  color: var(--text-primary);
}
.sub-toggle-btn:not(.active):hover {
  color: var(--text-secondary);
}

.loading, .error {
  text-align: center;
  padding: 48px;
  color: var(--text-muted);
}

.script-banner {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 24px;
  background: var(--bg-secondary);
  border-bottom: 1px solid var(--border);
  font-size: 12px;
}

.script-label {
  color: var(--text-muted);
  white-space: nowrap;
}

.script-path {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--text-secondary);
  background: var(--bg-primary);
  padding: 2px 8px;
  border-radius: 4px;
  cursor: pointer;
  user-select: all;
  transition: color 0.15s;
}

.script-path:hover {
  color: var(--accent-green);
}

.script-path:focus-visible {
  outline: 2px solid var(--accent-blue);
  outline-offset: 2px;
}

.copied-hint {
  color: var(--accent-green);
  font-size: 11px;
}

main {
  padding: 24px;
}

.toast-notification {
  position: fixed;
  bottom: 24px;
  left: 50%;
  transform: translateX(-50%);
  background: var(--bg-secondary);
  border: 1px solid var(--bg-tertiary);
  color: var(--text-primary);
  padding: 10px 20px;
  border-radius: 8px;
  font-size: 13px;
  z-index: 2000;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.4);
  pointer-events: none;
}
.toast-enter-active,
.toast-leave-active {
  transition: opacity 0.2s, transform 0.2s;
}
.toast-enter-from,
.toast-leave-to {
  opacity: 0;
  transform: translateX(-50%) translateY(8px);
}
</style>
