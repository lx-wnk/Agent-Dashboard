<script setup lang="ts">
import type { Agent } from '../types'
import type { AgentGrouping } from '../utils/agentGroup'
import { computed } from 'vue'
import AgentCard from './AgentCard.vue'
import GroupHeader from './shell/GroupHeader.vue'

const props = defineProps<{
  agents: Agent[]
  groups?: AgentGrouping[]
}>()

defineEmits<{ select: [agent: Agent], dismiss: [pid: number] }>()

// Use grouped rendering when groups are provided and at least one has a label.
const useGroups = computed(() =>
  !!props.groups && props.groups.some(g => g.label !== null),
)
</script>

<template>
  <template v-if="useGroups && groups">
    <div class="flex flex-col gap-4">
      <div v-for="group in groups" :key="group.key">
        <GroupHeader :label="group.label!" :agents="group.agents" />
        <div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3 mt-2">
          <AgentCard
            v-for="agent in group.agents"
            :key="agent.pid"
            :agent="agent"
            @select="$emit('select', agent)"
            @dismiss="$emit('dismiss', $event)"
          />
        </div>
      </div>
    </div>
  </template>
  <template v-else>
    <div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3">
      <AgentCard
        v-for="agent in agents"
        :key="agent.pid"
        :agent="agent"
        @select="$emit('select', agent)"
        @dismiss="$emit('dismiss', $event)"
      />
      <p v-if="agents.length === 0" class="col-span-full text-center py-12 text-fg-mute text-sm">
        No running Claude agents found.
      </p>
    </div>
  </template>
</template>
