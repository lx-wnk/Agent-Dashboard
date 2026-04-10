<script setup lang="ts">
import type { Agent } from '../types'
import AgentCard from './AgentCard.vue'

defineProps<{ agents: Agent[] }>()
defineEmits<{ select: [agent: Agent] }>()
</script>

<template>
  <div class="card-grid">
    <AgentCard
      v-for="agent in agents"
      :key="agent.pid"
      :agent="agent"
      @select="$emit('select', agent)"
    />
    <p v-if="agents.length === 0" class="empty">
      No running Claude agents found.
    </p>
  </div>
</template>

<style scoped>
.card-grid {
  display: grid;
  grid-template-columns: repeat(1, 1fr);
  gap: 12px;
}
@media (min-width: 768px) {
  .card-grid { grid-template-columns: repeat(2, 1fr); }
}
@media (min-width: 1200px) {
  .card-grid { grid-template-columns: repeat(3, 1fr); }
}
.empty {
  grid-column: 1 / -1;
  text-align: center;
  padding: 48px;
  color: var(--text-muted);
  font-size: 14px;
}
</style>
