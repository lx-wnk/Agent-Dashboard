<script setup lang="ts">
import { computed, ref } from 'vue'
import AgentCardGrid from './components/AgentCardGrid.vue'
import AgentModal from './components/AgentModal.vue'
import AgentTable from './components/AgentTable.vue'
import CostTrend from './components/CostTrend.vue'
import KanbanBoard from './components/KanbanBoard.vue'
import ResourceBar from './components/ResourceBar.vue'
import SessionList from './components/SessionList.vue'
import SpawnDialog from './components/SpawnDialog.vue'
import { useAgents } from './composables/useAgents'
import { useTheme } from './composables/useTheme'
import { formatTokens, totalTokenCount } from './utils/format'

const { agents, filteredAgents, selectedAgent, isLoading, error, searchQuery, viewMode, selectAgent } = useAgents()
const { theme, toggleTheme } = useTheme()
const showSpawnDialog = ref(false)
const showSessions = ref(false)
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

function onAgentSpawned(_pid: number) {
  // Agent will appear in the next polling cycle (~3s)
}

const totalCost = computed(() => agents.value.reduce((sum, a) => sum + a.costEstimate, 0))
const totalTokens = computed(() => agents.value.reduce((sum, a) => sum + totalTokenCount(a.tokenUsage), 0))
</script>

<template>
  <div class="app">
    <header class="app-header">
      <h1>Claude Agent Overview</h1>
      <span class="agent-count">{{ filteredAgents.length }} agent{{ filteredAgents.length !== 1 ? 's' : '' }}</span>
      <span v-if="totalCost > 0" class="header-stat">${{ totalCost.toFixed(2) }}</span>
      <span v-if="totalTokens > 0" class="header-stat">{{ formatTokens(totalTokens) }} tokens</span>
      <input
        v-model="searchQuery"
        class="header-search"
        type="text"
        placeholder="Search agents..."
      >
      <div class="view-toggle">
        <button
          class="toggle-btn"
          :class="{ active: viewMode === 'list' }"
          title="List view"
          @click="viewMode = 'list'"
        >
          ≡
        </button>
        <button
          class="toggle-btn"
          :class="{ active: viewMode === 'cards' }"
          title="Card view"
          @click="viewMode = 'cards'"
        >
          ⊞
        </button>
        <button
          class="toggle-btn"
          :class="{ active: viewMode === 'kanban' }"
          title="Kanban view"
          @click="viewMode = 'kanban'"
        >
          ▦
        </button>
      </div>
      <button class="theme-btn" :title="theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'" @click="toggleTheme">
        {{ theme === 'dark' ? '☀' : '☾' }}
      </button>
      <button class="sessions-btn" @click="showSessions = true">
        Sessions
      </button>
      <button class="spawn-btn" @click="showSpawnDialog = true">
        + New Agent
      </button>
    </header>
    <ResourceBar />
    <CostTrend />
    <div v-if="scriptPath" class="script-banner">
      <span class="script-label">Channel script:</span>
      <code class="script-path" tabindex="0" role="button" :title="copied ? 'Copied!' : 'Click to copy'" @click="copyScript" @keydown.enter="copyScript" @keydown.space.prevent="copyScript">{{ scriptPath }}</code>
      <span v-if="copied" class="copied-hint">Copied!</span>
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
      <KanbanBoard
        v-else-if="viewMode === 'kanban'"
        :agents="filteredAgents"
        @select="selectAgent"
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
    />
    <SpawnDialog
      :open="showSpawnDialog"
      @close="showSpawnDialog = false"
      @spawned="onAgentSpawned"
    />
    <SessionList
      :open="showSessions"
      :home-dir="homeDir"
      @close="showSessions = false"
      @spawned="onAgentSpawned"
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
  padding: 6px 10px;
  font-size: 14px;
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}
.toggle-btn.active {
  background: var(--accent-blue);
  color: white;
}
.toggle-btn:not(.active):hover {
  color: var(--text-primary);
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
</style>
