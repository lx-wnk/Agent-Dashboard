<script setup lang="ts">
import type { Agent } from '../types'
import type { AgentGrouping } from '../utils/agentGroup'
import { computed } from 'vue'
import AgentRow from './AgentRow.vue'
import GroupHeader from './shell/GroupHeader.vue'

const props = defineProps<{
  agents: Agent[]
  groups?: AgentGrouping[]
}>()

const emit = defineEmits<{
  select: [agent: Agent]
}>()

// Groups with non-null labels trigger the grouped rendering path.
const useGroups = computed(() =>
  !!props.groups && props.groups.some(g => g.label !== null),
)
</script>

<template>
  <div class="flex flex-col gap-1.5">
    <!-- Grouped rendering -->
    <template v-if="useGroups && groups">
      <div v-for="group in groups" :key="group.key" class="flex flex-col gap-1.5">
        <GroupHeader :label="group.label!" :agents="group.agents" class="mt-2 first:mt-0" />
        <AgentRow
          v-for="agent in group.agents"
          :key="agent.pid"
          :agent="agent"
          @select="emit('select', agent)"
        />
      </div>
    </template>

    <!-- Flat rendering -->
    <template v-else>
      <AgentRow
        v-for="agent in agents"
        :key="agent.pid"
        :agent="agent"
        @select="emit('select', agent)"
      />
      <p v-if="agents.length === 0" class="text-center py-12 text-fg-mute text-sm">
        No running Claude agents found.
      </p>
    </template>
  </div>
</template>
