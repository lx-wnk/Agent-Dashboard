<template>
  <div class="table-container">
    <table class="agent-table">
      <thead>
        <tr>
          <th>Status</th>
          <th>Project</th>
          <th>Current Action</th>
          <th>Uptime</th>
          <th>PID</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        <template v-for="agent in agents" :key="agent.pid">
          <AgentRow
            :agent="agent"
            :expanded="expandedPids.has(agent.pid)"
            @select="$emit('select', agent)"
            @toggle-subagents="toggleSubagents(agent.pid)"
          />
          <template v-if="expandedPids.has(agent.pid)">
            <SubAgentRow
              v-for="sub in agent.subagents"
              :key="sub.id"
              :subagent="sub"
            />
          </template>
        </template>
      </tbody>
    </table>
    <p v-if="agents.length === 0" class="empty">
      No running Claude agents found.
    </p>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import type { Agent } from '../types'
import AgentRow from './AgentRow.vue'
import SubAgentRow from './SubAgentRow.vue'

defineProps<{
  agents: Agent[]
}>()

defineEmits<{
  select: [agent: Agent]
}>()

const expandedPids = ref(new Set<number>())

function toggleSubagents(pid: number) {
  if (expandedPids.value.has(pid)) {
    expandedPids.value.delete(pid)
  } else {
    expandedPids.value.add(pid)
  }
}
</script>

<style scoped>
.table-container {
  overflow-x: auto;
}

.agent-table {
  width: 100%;
  border-collapse: collapse;
}

.agent-table th {
  padding: 8px 12px;
  text-align: left;
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: var(--text-muted);
  border-bottom: 1px solid var(--border);
  background: var(--bg-primary);
  position: sticky;
  top: 0;
  z-index: 1;
}

.empty {
  text-align: center;
  padding: 48px;
  color: var(--text-muted);
  font-size: 14px;
}
</style>
