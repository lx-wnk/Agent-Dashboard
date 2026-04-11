<script setup lang="ts">
import type { Agent } from '../types'
import { formatCost, formatTokens, formatUptime, shortModel, totalTokenCount } from '../utils/format'
import StatusBadge from './StatusBadge.vue'

defineProps<{
  agent: Agent
  expanded: boolean
}>()

defineEmits<{
  select: [agent: Agent]
  toggleSubagents: []
}>()
</script>

<template>
  <tr
    class="agent-row"
    tabindex="0"
    role="row"
    :aria-label="`Open details for ${agent.projectName}`"
    @click="$emit('select', agent)"
    @keydown.enter="$emit('select', agent)"
    @keydown.space.prevent="$emit('select', agent)"
  >
    <td class="col-status">
      <StatusBadge :status="agent.status" />
    </td>
    <td class="col-project">
      {{ agent.projectName }}
      <span v-if="agent.channelAvailable" class="channel-badge" title="Channel active">CH</span>
      <span v-if="agent.machine" class="machine-badge" :title="`Machine: ${agent.machine}`">{{ agent.machine }}</span>
    </td>
    <td class="col-action">
      {{ agent.currentAction || '—' }}
    </td>
    <td class="col-model">
      {{ shortModel(agent.model) }}
    </td>
    <td class="col-tokens">
      {{ formatTokens(totalTokenCount(agent.tokenUsage)) }}
    </td>
    <td class="col-cost">
      {{ formatCost(agent.costEstimate) }}
    </td>
    <td class="col-uptime">
      {{ formatUptime(agent.uptime) }}
    </td>
    <td class="col-pid">
      {{ agent.pid }}
    </td>
    <td class="col-toggle">
      <button
        v-if="agent.subagents.length > 0"
        class="toggle-btn"
        @click.stop="$emit('toggleSubagents')"
      >
        {{ expanded ? '▼' : '▶' }} {{ agent.subagents.length }}
      </button>
    </td>
  </tr>
</template>

<style scoped>
.agent-row {
  cursor: pointer;
  transition: background 0.15s;
}

.agent-row:hover {
  background: var(--bg-secondary);
}

.agent-row:focus-visible {
  outline: 2px solid var(--accent-blue);
  outline-offset: -2px;
}

.agent-row td {
  padding: 10px 12px;
  border-bottom: 1px solid var(--border);
  font-size: 13px;
}

.col-status { width: 100px; }
.col-project { color: var(--text-primary); font-weight: 500; }
.col-action { color: var(--text-secondary); max-width: 250px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.col-model { color: var(--text-muted); font-size: 12px; white-space: nowrap; }
.col-tokens { color: var(--text-muted); font-family: var(--font-mono); font-size: 12px; white-space: nowrap; }
.col-cost { color: var(--accent-green); font-family: var(--font-mono); font-size: 12px; white-space: nowrap; }
.col-uptime { color: var(--text-muted); width: 80px; }
.col-pid { color: var(--text-muted); width: 70px; font-family: var(--font-mono); font-size: 12px; }
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

.channel-badge {
  display: inline-block;
  margin-left: 6px;
  padding: 0 4px;
  font-size: 9px;
  font-weight: 600;
  color: var(--accent-green);
  border: 1px solid var(--accent-green);
  border-radius: 3px;
  vertical-align: middle;
  letter-spacing: 0.5px;
}

.machine-badge {
  display: inline-block;
  margin-left: 6px;
  padding: 0 4px;
  font-size: 9px;
  font-weight: 600;
  color: var(--accent-blue);
  border: 1px solid var(--accent-blue);
  border-radius: 3px;
  vertical-align: middle;
  letter-spacing: 0.5px;
  max-width: 100px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
