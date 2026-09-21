<script setup lang="ts">
import { computed } from 'vue'
import { useTasks } from '@/features/pipeline'
import LiveWorkRail from './LiveWorkRail.vue'

const { tasks } = useTasks({ autoStart: false })

// Named positively on purpose. The first version excluded done, backlog and
// ready, which silently let 'cancelled' and 'on_hold' through — ten cancelled
// tasks rendered under the heading "10 running". A list of what counts cannot
// grow a hole when a stage is added; a list of what does not, can.
const RUNNING_STAGES = new Set(['plan_review', 'implementation', 'self_review', 'finalization'])
const running = computed(() => tasks.value.filter(t => RUNNING_STAGES.has(t.currentStage)))
</script>

<template>
  <LiveWorkRail :tasks="running" />
</template>
