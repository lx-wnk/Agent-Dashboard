<script setup lang="ts">
import AppChip from '@/components/ui/AppChip.vue'
import StageOutputView from '@/features/pipeline/components/StageOutputView.vue'
import { useInjectedTaskDetails } from '@/features/pipeline/composables/taskModalContext'
import { useStageInjections } from '@/features/pipeline/composables/useStageInjections'
import { runStatusTone } from '@/utils/statusColors'
import { formatTaskDate } from '@/utils/taskFormat'

const { stageRuns } = useInjectedTaskDetails()
const { byStageRun, loading, denied, error } = useStageInjections(stageRuns)
</script>

<template>
  <section class="p-5">
    <div v-if="denied" data-testid="stage-injection-denied" class="rounded border border-warning-line bg-warning-soft text-warning-text px-3 py-2 text-[11px] mb-2">
      <strong>Memory pushes are hidden: <code>memory.read</code> is not granted at global scope.</strong>
      {{ denied }}
      This route always checks the global context — a project-scoped grant does not cover it. Add an <code>allow</code> grant for <code>memory.read</code> at <strong>global</strong> scope in the Grants panel.
    </div>
    <div v-else-if="error" data-testid="stage-injection-error" class="rounded border border-danger-line bg-danger-soft text-danger-text px-3 py-2 text-[11px] mb-2">
      {{ error }}
    </div>
    <div v-else-if="loading" data-testid="stage-injection-loading" class="text-fg-mute text-[11px] mb-2">
      Checking memory pushes...
    </div>
    <div v-if="stageRuns.length === 0" class="text-fg-mute text-xs text-center py-8">
      No stage runs yet.
    </div>
    <div v-for="run in stageRuns" v-else :key="run.id" class="px-3 py-2.5 bg-app rounded-md mb-2">
      <div class="flex items-center gap-2.5 mb-1">
        <span class="font-semibold text-xs text-fg">{{ run.stage }}</span>
        <span class="font-mono text-[11px] text-fg-mute">iter {{ run.iteration }}</span>
        <AppChip :tone="runStatusTone(run.status)" mono uppercase class="ml-auto">
          {{ run.status }}
        </AppChip>
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
      <div
        v-for="inj in (byStageRun[run.id] ?? [])"
        :key="inj.id"
        :data-testid="`stage-injection-${run.id}`"
        class="mt-1.5 text-[11px] text-fg-mute flex flex-wrap items-center gap-x-3 gap-y-1"
      >
        <span>memory push: <strong class="text-fg">{{ inj.entryIds.length }} entries</strong></span>
        <span>{{ inj.charsUsed }} / {{ inj.charBudget }} chars</span>
        <span>from {{ inj.candidateCount }} candidates</span>
      </div>
    </div>
  </section>
</template>
