<script setup lang="ts">
import type { Agent } from '../types'
import { computed } from 'vue'
import { formatCost, formatTokens, formatUptime, shortModel, totalTokenCount } from '../utils/format'
import MachineBadge from './MachineBadge.vue'
import PromptInput from './PromptInput.vue'
import StatusBadge from './StatusBadge.vue'

const props = defineProps<{ agent: Agent }>()
defineEmits<{ select: [agent: Agent] }>()

const totalTokens = computed(() => totalTokenCount(props.agent.tokenUsage))
</script>

<template>
  <div
    class="agent-card"
    tabindex="0"
    role="button"
    :aria-label="`Open details for ${agent.projectName}`"
    @click="$emit('select', agent)"
    @keydown.enter="$emit('select', agent)"
    @keydown.space.prevent="$emit('select', agent)"
  >
    <div class="card-titlebar">
      <div class="card-title-left">
        <StatusBadge :status="agent.status" />
        <span class="card-project">{{ agent.projectName }}</span>
        <span class="card-meta">{{ shortModel(agent.model) }} · {{ formatCost(agent.costEstimate) }}</span>
        <MachineBadge v-if="agent.machine" :machine="agent.machine" />
      </div>
      <div class="card-title-right">
        <span class="card-meta">{{ formatTokens(totalTokens) }} tok · {{ formatUptime(agent.uptime) }}</span>
      </div>
    </div>
    <div class="card-output">
      <template v-if="agent.lastOutput">
        {{ agent.lastOutput }}
      </template>
      <span v-else class="card-no-output">No output yet</span>
    </div>
    <PromptInput v-if="!agent.machine" :agent="agent" variant="compact" @click.stop @keydown.enter.stop @keydown.space.stop />
  </div>
</template>

<style scoped>
.agent-card {
  background: var(--bg-primary);
  border-radius: 8px;
  overflow: hidden;
  cursor: pointer;
  border: 1px solid var(--border);
  transition: border-color 0.15s, box-shadow 0.15s;
}
.agent-card:hover {
  border-color: var(--bg-tertiary);
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.3);
}
.agent-card:focus-visible {
  outline: 2px solid var(--accent-blue);
  outline-offset: -2px;
}
.card-titlebar {
  background: var(--bg-secondary);
  padding: 8px 12px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
}
.card-title-left {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.card-title-right { flex-shrink: 0; }
.card-project {
  font-weight: 600;
  font-size: 13px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.card-meta {
  font-size: 11px;
  color: var(--text-muted);
  white-space: nowrap;
}
.card-output {
  padding: 12px;
  font-size: 13px;
  line-height: 1.5;
  color: var(--text-secondary);
  max-height: 120px;
  overflow: hidden;
  position: relative;
  font-family: var(--font-mono);
}
.card-output::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  height: 32px;
  background: linear-gradient(transparent, var(--bg-primary));
  pointer-events: none;
}
.card-no-output {
  color: var(--text-muted);
  font-style: italic;
}

</style>
