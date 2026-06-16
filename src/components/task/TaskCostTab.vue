<script setup lang="ts">
import { useInjectedTask } from '../../composables/taskModalContext'
import { useTaskCostBreakdown } from '../../composables/useTaskCostBreakdown'
import StageCostWaterfall from '../StageCostWaterfall.vue'

const task = useInjectedTask()
const { costBreakdown, costLoading, costError } = useTaskCostBreakdown(task)
</script>

<template>
  <section class="p-5">
    <h3 class="text-[12px] font-semibold uppercase tracking-wider text-fg-mute mb-2">
      Cost breakdown
    </h3>
    <div v-if="costLoading" class="text-sm text-fg-mute">
      Loading...
    </div>
    <div v-else-if="costError" class="text-sm text-red-500 dark:text-red-400">
      {{ costError }}
    </div>
    <StageCostWaterfall v-else :rows="costBreakdown" />
  </section>
</template>
