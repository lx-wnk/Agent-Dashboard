<script setup lang="ts">
import type { SubAgent } from '../types'
import StatusBadge from './StatusBadge.vue'

defineProps<{
  subagents: SubAgent[]
}>()
</script>

<template>
  <div v-if="subagents.length > 0" class="subagent-list">
    <h4>Subagents ({{ subagents.length }})</h4>
    <div v-for="sa in subagents" :key="sa.id" class="subagent-item">
      <div class="subagent-header">
        <StatusBadge :status="sa.status" />
        <span class="subagent-id">{{ sa.id.substring(0, 16) }}</span>
      </div>
      <div v-if="sa.type !== 'unknown'" class="subagent-meta">
        {{ sa.type }}
      </div>
      <div v-if="sa.currentAction" class="subagent-action">
        Last tool: {{ sa.currentAction }}
      </div>
    </div>
  </div>
</template>

<style scoped>
h4 {
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: var(--text-muted);
  margin-bottom: 8px;
}

.subagent-item {
  padding: 8px;
  border-radius: 6px;
  background: var(--bg-primary);
  margin-bottom: 6px;
}

.subagent-header {
  display: flex;
  align-items: center;
  gap: 8px;
}

.subagent-id {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--text-secondary);
}

.subagent-meta {
  font-size: 11px;
  color: var(--text-muted);
  margin-top: 4px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.subagent-action {
  font-size: 11px;
  color: var(--text-muted);
  margin-top: 2px;
}
</style>
