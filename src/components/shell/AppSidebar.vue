<script setup lang="ts">
import type { ActiveView } from '../../composables/useViewState'
import { computed } from 'vue'
import { useSidebar } from '../../composables/useSidebar'
import { useViewState } from '../../composables/useViewState'
import { NAV_GROUPS, NAV_ITEMS } from '../../utils/navConfig'
import NavItem from './NavItem.vue'
import SidebarFooter from './SidebarFooter.vue'

const props = defineProps<{
  agentCount: number
  taskCount: number
  totalCostLabel: string
  totalTokensLabel: string
  todayCostLabel: string
  quotaPct: number
  theme: 'dark' | 'light'
}>()
const emit = defineEmits<{
  openSessions: []
  openSettings: []
  toggleTheme: []
}>()

const { expanded, pinned, togglePinned, setHovering } = useSidebar()
const { activeView } = useViewState()

const grouped = computed(() =>
  NAV_GROUPS.map(group => ({ group, items: NAV_ITEMS.filter(i => i.group === group) })))

function badgeFor(view: ActiveView): number | null {
  if (view === 'dashboard')
    return props.agentCount
  if (view === 'pipeline')
    return props.taskCount
  return null
}
</script>

<template>
  <nav
    aria-label="Primary"
    class="h-full bg-card border-r border-line flex flex-col py-3 transition-[width] duration-200"
    :class="expanded ? 'w-[220px] px-2' : 'w-[56px] px-1.5'"
    @mouseenter="setHovering(true)"
    @mouseleave="setHovering(false)"
  >
    <div class="flex items-center gap-2 px-1.5 pb-3 mb-2 border-b border-line">
      <div class="w-7 h-7 rounded-lg bg-accent shrink-0" aria-hidden="true" />
      <span v-if="expanded" class="text-[13px] font-semibold text-fg truncate">Agent Overview</span>
      <button
        type="button"
        data-testid="sidebar-pin"
        class="ml-auto text-fg-faint hover:text-fg text-[14px] rounded px-1 min-h-[28px] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-card"
        :aria-expanded="pinned"
        :aria-label="pinned ? 'Unpin sidebar' : 'Pin sidebar open'"
        @click="togglePinned"
      >
        <span aria-hidden="true">{{ pinned ? '«' : '»' }}</span>
      </button>
    </div>

    <div class="flex-1 flex flex-col gap-0.5 overflow-y-auto">
      <template v-for="g in grouped" :key="g.group">
        <div v-if="expanded" class="px-2 pt-3 pb-1 text-[9px] uppercase tracking-wider text-fg-faint font-bold">
          {{ g.group }}
        </div>
        <NavItem
          v-for="item in g.items"
          :key="item.view"
          :icon="item.icon"
          :label="item.label"
          :active="activeView === item.view"
          :expanded="expanded"
          @select="activeView = item.view"
        >
          <template v-if="badgeFor(item.view) !== null" #badge>
            <span class="text-[9px] bg-raised text-fg-mute rounded-full px-1.5 py-0.5">{{ badgeFor(item.view) }}</span>
          </template>
        </NavItem>
      </template>
    </div>

    <SidebarFooter
      :expanded="expanded"
      :total-cost-label="totalCostLabel"
      :total-tokens-label="totalTokensLabel"
      :today-cost-label="todayCostLabel"
      :quota-pct="quotaPct"
      :theme="theme"
      @open-sessions="emit('openSessions')"
      @open-settings="emit('openSettings')"
      @toggle-theme="emit('toggleTheme')"
    />
  </nav>
</template>
