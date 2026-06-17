<script setup lang="ts">
import type { Agent } from '../../types'
import { computed } from 'vue'
import { formatCost } from '../../utils/format'

const props = defineProps<{
  label: string
  agents: Agent[]
}>()

const totalCost = computed(() => props.agents.reduce((sum, a) => sum + a.costEstimate, 0))
</script>

<template>
  <div class="flex items-center gap-2 px-1 py-0.5">
    <span class="font-mono text-[11px] font-semibold text-fg-soft">{{ label }}</span>
    <span class="text-[11px] text-fg-faint">{{ agents.length }} {{ agents.length === 1 ? 'agent' : 'agents' }}</span>
    <span class="flex-1 h-px bg-line" />
    <span class="font-mono text-[11px] text-fg-faint">{{ formatCost(totalCost) }} today</span>
  </div>
</template>
