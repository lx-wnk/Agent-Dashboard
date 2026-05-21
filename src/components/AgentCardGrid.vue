<script setup lang="ts">
import type { Agent } from '../types'
import AgentCard from './AgentCard.vue'

defineProps<{ agents: Agent[] }>()
defineEmits<{ select: [agent: Agent] }>()
</script>

<template>
  <div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3">
    <AgentCard
      v-for="agent in agents"
      :key="agent.pid"
      :agent="agent"
      @select="$emit('select', agent)"
    />
    <p v-if="agents.length === 0" class="col-span-full text-center py-12 text-fg-mute text-sm">
      No running Claude agents found.
    </p>
  </div>
</template>
