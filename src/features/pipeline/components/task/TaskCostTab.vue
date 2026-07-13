<script setup lang="ts">
import StageCostWaterfall from '@/features/pipeline/components/StageCostWaterfall.vue'
import { useInjectedTask } from '@/features/pipeline/composables/taskModalContext'
import { useTaskCostBreakdown } from '@/features/pipeline/composables/useTaskCostBreakdown'

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
    <div v-else-if="costError" class="text-sm text-danger-text">
      {{ costError }}
    </div>
    <StageCostWaterfall v-else :rows="costBreakdown" />
  </section>
</template>
