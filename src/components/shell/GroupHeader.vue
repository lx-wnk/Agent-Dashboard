<script setup lang="ts">
import type { Agent } from '../../types'
import { computed } from 'vue'
import { formatCost } from '../../utils/format'

const props = defineProps<{
  label: string
  agents: Agent[]
  collapsed?: boolean
}>()

const emit = defineEmits<{ toggle: [] }>()

const totalCost = computed(() => props.agents.reduce((sum, a) => sum + a.costEstimate, 0))
const chevron = computed(() => props.collapsed ? '▶' : '▼')
</script>

<template>
  <button
    type="button"
    class="w-full flex items-center gap-2 px-1 py-0.5 focus-visible:outline-2 focus-visible:outline-ring rounded"
    :aria-expanded="!collapsed"
    :aria-label="`Toggle ${label} group`"
    data-testid="group-header-toggle"
    @click="emit('toggle')"
  >
    <span class="text-fg-soft text-xs leading-none w-4 inline-block text-center" aria-hidden="true">{{ chevron }}</span>
    <span class="font-mono text-[11px] font-semibold text-fg-soft">{{ label }}</span>
    <span class="text-[11px] text-fg-faint">{{ agents.length }} {{ agents.length === 1 ? 'agent' : 'agents' }}</span>
    <span class="flex-1 h-px bg-line" />
    <span class="font-mono text-[11px] text-fg-faint">{{ formatCost(totalCost) }} today</span>
  </button>
</template>
