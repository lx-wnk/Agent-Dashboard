<script setup lang="ts">
import type { Agent } from '../types'
import type { AgentGroup, AgentGrouping } from '../utils/agentGroup'
import { computed, ref, watch } from 'vue'
import AgentCard from './AgentCard.vue'
import GroupHeader from './shell/GroupHeader.vue'

const props = defineProps<{
  agents: Agent[]
  groups?: AgentGrouping[]
  groupBy?: AgentGroup
}>()

defineEmits<{ select: [agent: Agent], dismiss: [pid: number] }>()

// Use grouped rendering when groups are provided and at least one has a label.
const useGroups = computed(() =>
  !!props.groups && props.groups.some(g => g.label !== null),
)

// Collapsed state is namespaced per grouping mode: the component is reused (not
// remounted) across mode switches, so an unscoped key would leak collapse state
// between project/status/model groupings whose key spaces overlap.
const STORAGE_PREFIX = 'agent-dashboard-collapsed-groups'
const storageKey = computed(() =>
  props.groupBy ? `${STORAGE_PREFIX}:${props.groupBy}` : STORAGE_PREFIX,
)

function readStoredKeys(key: string): string[] {
  if (typeof localStorage === 'undefined')
    return []
  try {
    const parsed = JSON.parse(localStorage.getItem(key) ?? '[]')
    return Array.isArray(parsed) ? parsed.filter((k): k is string => typeof k === 'string') : []
  }
  catch {
    return []
  }
}

function hasStoredState(key: string): boolean {
  return typeof localStorage !== 'undefined' && localStorage.getItem(key) !== null
}

const collapsedKeys = ref<Set<string>>(new Set(readStoredKeys(storageKey.value)))
const defaultAppliedModes = ref<Set<string>>(
  new Set(hasStoredState(storageKey.value) ? [storageKey.value] : []),
)

function isCollapsed(key: string): boolean {
  return collapsedKeys.value.has(key)
}

// Persistence is explicit (not a watch on collapsedKeys) so a programmatic
// reset on mode switch is never mistaken for user-saved state.
function persist(): void {
  if (typeof localStorage === 'undefined')
    return
  localStorage.setItem(storageKey.value, JSON.stringify(Array.from(collapsedKeys.value)))
}

function toggleGroup(key: string): void {
  const next = new Set(collapsedKeys.value)
  if (next.has(key))
    next.delete(key)
  else
    next.add(key)
  collapsedKeys.value = next
  persist()
}

// On a mode switch, load that mode's persisted collapse state (without
// persisting); the groups watcher re-applies the first-load default once a
// populated frame arrives for a mode that has none.
watch(storageKey, (key) => {
  collapsedKeys.value = new Set(readStoredKeys(key))
})

// First populated frame per mode with no stored state: collapse every group
// except the first non-empty one (groupAgents sorts status groups by priority),
// then persist. Thereafter the stored/user state wins.
watch(() => props.groups, (groups) => {
  if (!useGroups.value || !groups)
    return
  const key = storageKey.value
  if (defaultAppliedModes.value.has(key) || hasStoredState(key))
    return
  const labeled = groups.filter(g => g.label !== null)
  if (labeled.length === 0)
    return
  const open = labeled.find(g => g.agents.length > 0) ?? labeled[0]
  collapsedKeys.value = new Set(labeled.filter(g => g.key !== open.key).map(g => g.key))
  defaultAppliedModes.value = new Set(defaultAppliedModes.value).add(key)
  persist()
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
