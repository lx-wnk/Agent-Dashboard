<script setup lang="ts">
import type { DashboardLayout } from '../../composables/useViewState'
import type { AgentGroup, AgentSort } from '../../utils/agentGroup'
import { computed } from 'vue'
import { AGENT_SORT_OPTIONS, agentGroupOptions, resolveGroup } from '../../utils/agentGroup'
import AppSelect from '../ui/AppSelect.vue'

const props = defineProps<{
  layout: DashboardLayout
  spawner: string
  project: string
  sortBy: AgentSort
  groupBy: AgentGroup
  projectOptions: Array<{ value: string, label: string }>
  spawnerOptions: Array<{ value: string, label: string }>
}>()

defineEmits<{
  'update:layout': [value: DashboardLayout]
  'update:spawner': [value: string]
  'update:project': [value: string]
  'update:sortBy': [value: AgentSort]
  'update:groupBy': [value: AgentGroup]
}>()

const groupOptions = computed(() => agentGroupOptions(props.spawner))
const effectiveGroup = computed(() => resolveGroup(props.groupBy, props.spawner))
</script>

<template>
  <div class="flex items-center gap-2 flex-wrap px-1 py-2">
    <!-- Filters: Project · Spawner -->
    <div class="flex items-center gap-2 flex-wrap">
      <AppSelect
        :model-value="project"
        :options="projectOptions"
        aria-label="Filter by project"
        data-testid="select-project"
        @update:model-value="$emit('update:project', $event)"
      />
      <AppSelect
        :model-value="spawner"
        :options="spawnerOptions"
        aria-label="Filter by spawner"
        data-testid="select-spawner"
        @update:model-value="$emit('update:spawner', $event)"
      />
    </div>

    <!-- Divider: filters / view-controls -->
    <span aria-hidden="true" class="w-px self-stretch min-h-[20px] bg-line mx-1" />

    <!-- View controls: Sort · Group -->
    <div class="flex items-center gap-2 flex-wrap">
      <span class="flex items-center gap-1.5">
        <span aria-hidden="true" class="text-[13px] text-fg-faint" title="Sort">⇅</span>
        <AppSelect
          :model-value="sortBy"
          :options="AGENT_SORT_OPTIONS"
          aria-label="Sort agents"
          data-testid="select-sort"
          @update:model-value="$emit('update:sortBy', $event)"
        />
      </span>
      <AppSelect
        :model-value="effectiveGroup"
        :options="groupOptions"
        aria-label="Group agents"
        data-testid="select-group"
        @update:model-value="$emit('update:groupBy', $event)"
      />
    </div>

    <!-- Density toggle: Comfortable / Compact — far right -->
    <div
      role="group"
      aria-label="Density"
      class="ml-auto flex bg-raised rounded-lg overflow-hidden p-0.5 gap-0.5"
    >
      <button
        type="button"
        data-testid="layout-cards"
        class="px-2.5 py-1 text-xs rounded-md transition-colors focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:ring-offset-1 focus-visible:ring-offset-raised"
        :class="layout === 'cards' ? 'bg-card text-fg shadow-sm' : 'text-fg-mute hover:text-fg'"
        :aria-pressed="layout === 'cards'"
        @click="$emit('update:layout', 'cards')"
      >
        Comfortable
      </button>
      <button
        type="button"
        data-testid="layout-list"
        class="px-2.5 py-1 text-xs rounded-md transition-colors focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:ring-offset-1 focus-visible:ring-offset-raised"
        :class="layout === 'list' ? 'bg-card text-fg shadow-sm' : 'text-fg-mute hover:text-fg'"
        :aria-pressed="layout === 'list'"
        @click="$emit('update:layout', 'list')"
      >
        Compact
      </button>
    </div>
  </div>
</template>
