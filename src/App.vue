<template>
  <div class="app">
    <header class="app-header">
      <h1>Claude Agent Overview</h1>
      <span class="agent-count">{{ agents.length }} agent{{ agents.length !== 1 ? 's' : '' }}</span>
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
import { useAgents } from './composables/useAgents'
import AgentTable from './components/AgentTable.vue'
import AgentDetail from './components/AgentDetail.vue'

const { agents, selectedAgent, isLoading, error, selectAgent } = useAgents()
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

.loading, .error {
  text-align: center;
  padding: 48px;
  color: var(--text-muted);
}

main {
  padding: 24px;
}
</style>
