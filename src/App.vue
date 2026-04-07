<template>
  <div class="app">
    <header class="app-header">
      <h1>Claude Agent Overview</h1>
      <span class="agent-count">{{ agents.length }} agent{{ agents.length !== 1 ? 's' : '' }}</span>
      <span class="header-stat" v-if="totalCost > 0">${{ totalCost.toFixed(2) }}</span>
      <span class="header-stat" v-if="totalTokens > 0">{{ formatTokens(totalTokens) }} tokens</span>
      <button class="sessions-btn" @click="showSessions = true">Sessions</button>
      <button class="spawn-btn" @click="showSpawnDialog = true">+ New Agent</button>
    </header>
    <ResourceBar />
    <div class="script-banner" v-if="scriptPath">
      <span class="script-label">Channel script:</span>
      <code class="script-path" @click="copyScript" :title="copied ? 'Copied!' : 'Click to copy'">{{ scriptPath }}</code>
      <span v-if="copied" class="copied-hint">Copied!</span>
    </div>
    <main>
      <p v-if="isLoading" class="loading">Loading agents...</p>
      <p v-else-if="error" class="error">Error: {{ error }}</p>
      <AgentTable
        v-else
        :agents="agents"
        @select="selectAgent"
      />
    </main>
    <AgentDetail
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
      @close="showSessions = false"
      @spawned="onAgentSpawned"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useAgents } from './composables/useAgents'
import { formatTokens } from './utils/format'
import AgentTable from './components/AgentTable.vue'
import AgentDetail from './components/AgentDetail.vue'
import SpawnDialog from './components/SpawnDialog.vue'
import SessionList from './components/SessionList.vue'
import ResourceBar from './components/ResourceBar.vue'

const { agents, selectedAgent, isLoading, error, selectAgent } = useAgents()
const showSpawnDialog = ref(false)
const showSessions = ref(false)
const scriptPath = ref('')
const copied = ref(false)

fetch('/api/config').then(r => r.json()).then(d => { scriptPath.value = d.scriptPath }).catch(() => {})

function copyScript() {
  navigator.clipboard.writeText(scriptPath.value)
  copied.value = true
  setTimeout(() => { copied.value = false }, 2000)
}

function onAgentSpawned(_pid: number) {
  // Agent will appear in the next polling cycle (~3s)
}

const totalCost = computed(() => agents.value.reduce((sum, a) => sum + a.costEstimate, 0))
const totalTokens = computed(() => agents.value.reduce((sum, a) => {
  const u = a.tokenUsage
  return sum + u.inputTokens + u.outputTokens + u.cacheReadTokens + u.cacheCreationTokens
}, 0))
</script>

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
  --border: #1e293b;
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
  font-family: monospace;
}

.sessions-btn {
  margin-left: auto;
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
  font-family: monospace;
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

.copied-hint {
  color: var(--accent-green);
  font-size: 11px;
}

main {
  padding: 24px;
}
</style>
