<script setup lang="ts">
import type { PanelState } from '@/features/cockpit'
import { computed, onMounted } from 'vue'
import { useTodayCost } from '@/composables/useTodayCost'
import { CockpitPanel } from '@/features/cockpit'
import { formatCost } from '@/utils/format'

const { todayUsd, start } = useTodayCost()
onMounted(start)
const state = computed<PanelState>(() => (todayUsd.value === null ? 'loading' : 'ready'))
</script>

<template>
  <CockpitPanel id="cost-today" title="Today" :state="state">
    <p data-testid="cost-today-value" class="text-[24px] font-semibold leading-none text-fg">
      {{ formatCost(todayUsd ?? 0) }}
    </p>
  </CockpitPanel>
</template>
