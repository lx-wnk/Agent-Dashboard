<script setup lang="ts">
import type { DashboardLayout } from '../../composables/useViewState'
import type { AgentGroup, AgentSort } from '../../utils/agentGroup'
import { AGENT_GROUP_OPTIONS, AGENT_SORT_OPTIONS } from '../../utils/agentGroup'
import AppSelect from '../ui/AppSelect.vue'

defineProps<{
  layout: DashboardLayout
  hideNonClaude: boolean
  project: string
  sortBy: AgentSort
  groupBy: AgentGroup
  projectOptions: Array<{ value: string; label: string }>
  count: number
  attentionCount: number
}>()

defineEmits<{
  'update:layout': [value: DashboardLayout]
  'update:hideNonClaude': [value: boolean]
  'update:project': [value: string]
  'update:sortBy': [value: AgentSort]
  'update:groupBy': [value: AgentGroup]
}>()
</script>

<template>
  <div class="flex items-center gap-3 flex-wrap px-1 py-2">
    <!-- Project / Sort / Group controls -->
    <div class="flex items-center gap-3 flex-wrap">
      <label class="flex items-center gap-1.5">
        <span class="text-xs text-fg-mute">Project</span>
        <AppSelect
          :model-value="project"
          :options="projectOptions"
          aria-label="Filter by project"
          data-testid="select-project"
          @update:model-value="$emit('update:project', $event as string)"
        />
      </label>

      <label class="flex items-center gap-1.5">
        <span class="text-xs text-fg-mute">Sort by</span>
        <AppSelect
          :model-value="sortBy"
          :options="AGENT_SORT_OPTIONS as unknown as Array<{ value: string; label: string }>"
          aria-label="Sort agents"
          data-testid="select-sort"
          @update:model-value="$emit('update:sortBy', $event as AgentSort)"
        />
      </label>

      <label class="flex items-center gap-1.5">
        <span class="text-xs text-fg-mute">Group by</span>
        <AppSelect
          :model-value="groupBy"
          :options="AGENT_GROUP_OPTIONS as unknown as Array<{ value: string; label: string }>"
          aria-label="Group agents"
          data-testid="select-group"
          @update:model-value="$emit('update:groupBy', $event as AgentGroup)"
        />
      </label>
    </div>

    <!-- Agent count + attention -->
    <span class="ml-auto text-xs text-fg-faint">
      {{ count }} {{ count === 1 ? 'agent' : 'agents' }}{{ attentionCount ? ` · ${attentionCount} need you` : '' }}
    </span>

    <!-- Layout + Claude-only controls -->
    <div role="group" aria-label="Layout" class="flex bg-raised rounded-lg overflow-hidden p-0.5 gap-0.5">
      <button
        type="button"
        data-testid="layout-cards"
        class="px-2.5 py-1 text-xs rounded-md transition-colors focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:ring-offset-1 focus-visible:ring-offset-raised"
        :class="layout === 'cards' ? 'bg-card text-fg shadow-sm' : 'text-fg-mute hover:text-fg'"
        :aria-pressed="layout === 'cards'"
        @click="$emit('update:layout', 'cards')"
      >
        ⊞ Cards
      </button>
      <button
        type="button"
        data-testid="layout-list"
        class="px-2.5 py-1 text-xs rounded-md transition-colors focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:ring-offset-1 focus-visible:ring-offset-raised"
        :class="layout === 'list' ? 'bg-card text-fg shadow-sm' : 'text-fg-mute hover:text-fg'"
        :aria-pressed="layout === 'list'"
        @click="$emit('update:layout', 'list')"
      >
        ≡ List
      </button>
    </div>
    <button
      type="button"
      data-testid="claude-only"
      class="border border-line px-2.5 py-1 text-xs rounded-lg transition-colors focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-card"
      :class="hideNonClaude ? 'bg-accent text-white border-transparent' : 'text-fg-mute hover:text-fg'"
      :aria-pressed="hideNonClaude"
      @click="$emit('update:hideNonClaude', !hideNonClaude)"
    >
      Claude only
    </button>
  </div>
</template>
