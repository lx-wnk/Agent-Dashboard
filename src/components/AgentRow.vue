<template>
  <tr class="agent-row" @click="$emit('select', agent)">
    <td class="col-status">
      <StatusBadge :status="agent.status" />
    </td>
    <td class="col-project">{{ agent.projectName }}</td>
    <td class="col-action">{{ agent.currentAction || '—' }}</td>
    <td class="col-uptime">{{ formatUptime(agent.uptime) }}</td>
    <td class="col-pid">{{ agent.pid }}</td>
    <td class="col-toggle">
      <button
        v-if="agent.subagents.length > 0"
        class="toggle-btn"
        @click.stop="$emit('toggle-subagents')"
      >
        {{ expanded ? '▼' : '▶' }} {{ agent.subagents.length }}
      </button>
    </td>
  </tr>
</template>

<script setup lang="ts">
import type { Agent } from '../types'
import StatusBadge from './StatusBadge.vue'

defineProps<{
  agent: Agent
  expanded: boolean
}>()

defineEmits<{
  select: [agent: Agent]
  'toggle-subagents': []
}>()

function formatUptime(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`
  return `${Math.floor(seconds / 86400)}d ${Math.floor((seconds % 86400) / 3600)}h`
}
</script>

<style scoped>
.agent-row {
  cursor: pointer;
  transition: background 0.15s;
}

.agent-row:hover {
  background: var(--bg-secondary);
}

.agent-row td {
  padding: 10px 12px;
  border-bottom: 1px solid var(--border);
  font-size: 13px;
}

.col-status { width: 100px; }
.col-project { color: var(--text-primary); font-weight: 500; }
.col-action { color: var(--text-secondary); max-width: 300px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.col-uptime { color: var(--text-muted); width: 80px; }
.col-pid { color: var(--text-muted); width: 70px; font-family: monospace; font-size: 12px; }
.col-toggle { width: 50px; text-align: center; }

.toggle-btn {
  background: none;
  border: none;
  color: var(--text-muted);
  cursor: pointer;
  font-size: 11px;
  padding: 2px 6px;
  border-radius: 4px;
}

.toggle-btn:hover {
  background: var(--bg-tertiary);
  color: var(--text-primary);
}
</style>
