<script setup lang="ts">
import type { DashboardLayout } from '../../composables/useViewState'
import type { AgentGroup, AgentSort } from '../../utils/agentGroup'
import { computed } from 'vue'
import { AGENT_SORT_OPTIONS, agentGroupOptions, resolveGroup } from '../../utils/agentGroup'
import AppSelect from '../ui/AppSelect.vue'
import ToolbarPopover from './ToolbarPopover.vue'

const props = defineProps<{
  layout: DashboardLayout
  spawner: string
  project: string
  sortBy: AgentSort
  groupBy: AgentGroup
  searchQuery: string
  projectOptions: Array<{ value: string, label: string }>
  spawnerOptions: Array<{ value: string, label: string }>
  totalCount: number
  shownCount: number
}>()

const emit = defineEmits<{
  'update:layout': [value: DashboardLayout]
  'update:spawner': [value: string]
  'update:project': [value: string]
  'update:sortBy': [value: AgentSort]
  'update:groupBy': [value: AgentGroup]
  'update:searchQuery': [value: string]
}>()

const groupOptions = computed(() => agentGroupOptions(props.spawner))
const effectiveGroup = computed(() => resolveGroup(props.groupBy, props.spawner))

const projectLabel = computed(() =>
  props.projectOptions.find(o => o.value === props.project)?.label ?? props.project)
const spawnerLabel = computed(() =>
  props.spawnerOptions.find(o => o.value === props.spawner)?.label ?? props.spawner)

// A spawner filter is only offerable once spawners exist; with none configured
// the select would be a dead control listing "All spawners" alone.
const hasSpawners = computed(() => props.spawnerOptions.length > 1)

interface ActiveFilter {
  key: 'search' | 'project' | 'spawner'
  label: string
  clear: () => void
}

// The search is a filter like any other: it narrows the same roster, so it
// belongs in the same summary and is cleared by the same "Clear all".
const activeFilters = computed<ActiveFilter[]>(() => {
  const list: ActiveFilter[] = []
  if (props.searchQuery)
    list.push({ key: 'search', label: `Search: "${props.searchQuery}"`, clear: () => emit('update:searchQuery', '') })
  if (props.project !== 'all')
    list.push({ key: 'project', label: `Project: ${projectLabel.value}`, clear: () => emit('update:project', 'all') })
  if (props.spawner !== 'all')
    list.push({ key: 'spawner', label: `Spawner: ${spawnerLabel.value}`, clear: () => emit('update:spawner', 'all') })
  return list
})

// The badge counts the dropdown's own filters; the search carries its own
// visible field, so counting it there would double-report it.
const menuFilterCount = computed(() =>
  activeFilters.value.filter(f => f.key !== 'search').length)

function clearAll(): void {
  for (const filter of activeFilters.value)
    filter.clear()
}
</script>

<template>
  <div class="flex flex-col gap-1.5 px-1 py-2">
    <div class="flex items-center gap-2 flex-wrap">
      <!-- Narrow the set: search · filters -->
      <div class="relative flex items-center">
        <span aria-hidden="true" class="absolute left-2.5 text-[11px] text-fg-faint">🔍</span>
        <input
          :value="searchQuery"
          type="search"
          aria-label="Search agents"
          placeholder="Search agents…"
          data-testid="toolbar-search"
          class="w-[180px] rounded-lg border border-line bg-app py-1 pl-7 pr-2 text-xs text-fg placeholder:text-fg-faint transition-[width,border-color] duration-200 focus-visible:w-[240px] focus-visible:border-accent focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent"
          :class="searchQuery ? 'border-accent' : ''"
          @input="$emit('update:searchQuery', ($event.target as HTMLInputElement).value)"
        >
      </div>

      <ToolbarPopover
        label="Filter"
        aria-label="Filter agents"
        icon="⛃"
        :badge="menuFilterCount"
        :active="menuFilterCount > 0"
        data-testid="filter-menu"
      >
        <div class="flex flex-col gap-3">
          <label class="flex flex-col gap-1 text-[11px] font-semibold uppercase tracking-wider text-fg-faint">
            Project
            <AppSelect
              :model-value="project"
              :options="projectOptions"
              size="compact"
              aria-label="Filter by project"
              data-testid="select-project"
              @update:model-value="$emit('update:project', $event)"
            />
          </label>
          <label v-if="hasSpawners" class="flex flex-col gap-1 text-[11px] font-semibold uppercase tracking-wider text-fg-faint">
            Spawner
            <AppSelect
              :model-value="spawner"
              :options="spawnerOptions"
              size="compact"
              aria-label="Filter by spawner"
              data-testid="select-spawner"
              @update:model-value="$emit('update:spawner', $event)"
            />
          </label>
        </div>
      </ToolbarPopover>

      <span aria-hidden="true" class="w-px self-stretch min-h-[20px] bg-line mx-1" />

      <!-- Arrange what is left: sort · group · overflow -->
      <span class="flex items-center gap-1.5">
        <span aria-hidden="true" class="text-[11px] uppercase tracking-wider text-fg-faint">Sort</span>
        <AppSelect
          :model-value="sortBy"
          :options="AGENT_SORT_OPTIONS"
          size="compact"
          aria-label="Sort agents"
          data-testid="select-sort"
          @update:model-value="$emit('update:sortBy', $event)"
        />
      </span>
      <span class="flex items-center gap-1.5">
        <span aria-hidden="true" class="text-[11px] uppercase tracking-wider text-fg-faint">Group</span>
        <AppSelect
          :model-value="effectiveGroup"
          :options="groupOptions"
          size="compact"
          aria-label="Group agents"
          data-testid="select-group"
          @update:model-value="$emit('update:groupBy', $event)"
        />
      </span>
      <ToolbarPopover
        v-slot="{ close }"
        label="⋮"
        aria-label="More view options"
        align="end"
        :show-caret="false"
        data-testid="view-overflow"
      >
        <fieldset class="flex flex-col gap-1">
          <legend class="mb-1 text-[11px] font-semibold uppercase tracking-wider text-fg-faint">
            Density
          </legend>
          <button
            v-for="option in ([['cards', 'Comfortable'], ['list', 'Compact']] as const)"
            :key="option[0]"
            type="button"
            :data-testid="option[0] === 'cards' ? 'layout-cards' : 'layout-list'"
            class="flex items-center gap-2 rounded-md px-2 py-1 text-left text-xs transition-colors focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent"
            :class="layout === option[0] ? 'bg-accent-soft text-accent font-semibold' : 'text-fg-mute hover:bg-raised hover:text-fg'"
            :aria-pressed="layout === option[0]"
            @click="$emit('update:layout', option[0]); close(true)"
          >
            <span aria-hidden="true">{{ layout === option[0] ? '●' : '○' }}</span>{{ option[1] }}
          </button>
        </fieldset>
      </ToolbarPopover>

      <span class="ml-auto text-xs text-fg-faint" role="status" data-testid="agent-count">
        <b class="font-semibold text-fg-mute">{{ shownCount }}</b>
        <template v-if="activeFilters.length"> / {{ totalCount }}</template>
        agents
      </span>
    </div>

    <!-- Applied filters, mirrored out of the controls so the state is readable at a glance -->
    <div v-if="activeFilters.length" class="flex items-center gap-1.5 flex-wrap" data-testid="active-filters">
      <span
        v-for="filter in activeFilters"
        :key="filter.key"
        class="inline-flex items-center gap-1 rounded-full bg-accent-soft py-0.5 pl-2.5 pr-1 text-[11px] text-accent"
      >
        {{ filter.label }}
        <button
          type="button"
          class="rounded-full px-1 leading-none hover:bg-accent hover:text-accent-contrast focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent"
          :aria-label="`Clear ${filter.label}`"
          :data-testid="`clear-${filter.key}`"
          @click="filter.clear()"
        >×</button>
      </span>
      <button
        type="button"
        class="rounded px-1.5 py-0.5 text-[11px] text-accent hover:underline focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent"
        data-testid="clear-all-filters"
        @click="clearAll"
      >
        Clear all
      </button>
    </div>
  </div>
</template>
