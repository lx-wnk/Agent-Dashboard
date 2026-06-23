<script setup lang="ts">
import type { Agent } from '../types'
import type { AgentGrouping } from '../utils/agentGroup'
import { computed, ref, watch } from 'vue'
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

const STORAGE_KEY = 'agent-dashboard-collapsed-groups'

function readStoredKeys(): string[] {
  if (typeof localStorage === 'undefined')
    return []
  try {
    return JSON.parse(localStorage.getItem(STORAGE_KEY) ?? '[]')
  }
  catch {
    return []
  }
}

function hasStoredState(): boolean {
  return typeof localStorage !== 'undefined' && localStorage.getItem(STORAGE_KEY) !== null
}

const collapsedKeys = ref<Set<string>>(new Set(readStoredKeys()))

function isCollapsed(key: string): boolean {
  return collapsedKeys.value.has(key)
}

function toggleGroup(key: string): void {
  const next = new Set(collapsedKeys.value)
  if (next.has(key))
    next.delete(key)
  else
    next.add(key)
  collapsedKeys.value = next
}

watch(collapsedKeys, (value) => {
  if (typeof localStorage === 'undefined')
    return
  localStorage.setItem(STORAGE_KEY, JSON.stringify(Array.from(value)))
})

// On first ever load (no stored state), collapse every group except the first
// non-empty one. groupAgents sorts status groups by priority, so groups[0] is
// Active → else Waiting → else Idle → else Finished. Applied once on the first
// populated frame and persisted; thereafter the stored/user state wins.
const defaultApplied = ref(hasStoredState())

watch(() => props.groups, (groups) => {
  if (defaultApplied.value || !useGroups.value || !groups)
    return
  const labeled = groups.filter(g => g.label !== null)
  if (labeled.length === 0)
    return
  collapsedKeys.value = new Set(labeled.slice(1).map(g => g.key))
  defaultApplied.value = true
}, { immediate: true })
</script>

<template>
  <template v-if="useGroups && groups">
    <div class="flex flex-col gap-4">
      <div v-for="group in groups" :key="group.key">
        <GroupHeader
          :label="group.label!"
          :agents="group.agents"
          :collapsed="isCollapsed(group.key)"
          @toggle="toggleGroup(group.key)"
        />
        <div
          v-show="!isCollapsed(group.key)"
          class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3 mt-2"
          data-testid="group-card-grid"
        >
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
