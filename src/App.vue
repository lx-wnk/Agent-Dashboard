<template>
  <div class="app">
    <header class="app-header">
      <h1>Claude Agent Overview</h1>
      <span class="agent-count">{{ agents.length }} agent{{ agents.length !== 1 ? 's' : '' }}</span>
      <span class="header-stat" v-if="totalCost > 0">${{ totalCost.toFixed(2) }}</span>
      <span class="header-stat" v-if="totalTokens > 0">{{ formatTokens(totalTokens) }} tokens</span>
    </header>
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
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useAgents } from './composables/useAgents'
import AgentTable from './components/AgentTable.vue'
import AgentDetail from './components/AgentDetail.vue'

const { agents, selectedAgent, isLoading, error, selectAgent } = useAgents()

const totalCost = computed(() => agents.value.reduce((sum, a) => sum + a.costEstimate, 0))
const totalTokens = computed(() => agents.value.reduce((sum, a) => {
  const u = a.tokenUsage
  return sum + u.inputTokens + u.outputTokens + u.cacheReadTokens + u.cacheCreationTokens
}, 0))

function formatTokens(n: number): string {
  if (n < 1000) return String(n)
  if (n < 1_000_000) return `${(n / 1000).toFixed(1)}k`
  return `${(n / 1_000_000).toFixed(1)}M`
}
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

.loading, .error {
  text-align: center;
  padding: 48px;
  color: var(--text-muted);
}

main {
  padding: 24px;
}
</style>
