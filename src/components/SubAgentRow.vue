<template>
  <tr class="subagent-row">
    <td class="col-status">
      <StatusBadge :status="subagent.status" />
    </td>
    <td class="col-type" colspan="2">
      <span class="indent">↳</span>
      {{ subagent.type === 'unknown' ? subagent.id.substring(0, 16) : subagent.type }}
    </td>
    <td class="col-action">{{ subagent.currentAction || '—' }}</td>
    <td colspan="5"></td>
  </tr>
</template>

<script setup lang="ts">
import type { SubAgent } from '../types'
import StatusBadge from './StatusBadge.vue'

defineProps<{
  subagent: SubAgent
}>()
</script>

<style scoped>
.subagent-row td {
  padding: 6px 12px;
  border-bottom: 1px solid var(--border);
  font-size: 12px;
  color: var(--text-muted);
  background: rgba(30, 41, 59, 0.3);
}

.indent {
  color: var(--text-muted);
  margin-right: 6px;
}

.col-type {
  padding-left: 36px;
  max-width: 350px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.col-action {
  color: var(--text-muted);
}
</style>
