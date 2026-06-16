<script setup lang="ts">
import { useInjectedTaskDetails } from '../../composables/taskModalContext'
import { runStatusChipClass } from '../../utils/statusColors'
import { formatTaskDate } from '../../utils/taskFormat'
import StageOutputView from '../StageOutputView.vue'

const { stageRuns } = useInjectedTaskDetails()
</script>

<template>
  <section class="p-5">
    <div v-if="stageRuns.length === 0" class="text-fg-mute text-xs text-center py-8">
      No stage runs yet.
    </div>
    <div v-for="run in stageRuns" v-else :key="run.id" class="px-3 py-2.5 bg-app rounded-md mb-2">
      <div class="flex items-center gap-2.5 mb-1">
        <span class="font-semibold text-xs text-fg">{{ run.stage }}</span>
        <span class="font-mono text-[11px] text-fg-mute">iter {{ run.iteration }}</span>
        <span class="text-[10px] px-1.5 py-px rounded uppercase ml-auto font-mono" :class="runStatusChipClass(run.status)">{{ run.status }}</span>
      </div>
      <div v-if="run.sessionName" class="text-[11px] text-fg-mute mt-0.5">
        session: <code>{{ run.sessionName }}</code>
      </div>
      <div class="text-[11px] text-fg-mute mt-0.5">
        started {{ formatTaskDate(run.startedAt) }} · ended {{ formatTaskDate(run.endedAt) }}
      </div>
      <div v-if="run.output" class="mt-1.5 text-[11px]">
        <StageOutputView :stage="run.stage" :output="run.output" :status="run.status" />
      </div>
    </div>
  </section>
</template>
