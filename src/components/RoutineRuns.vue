<script setup lang="ts">
import type { RoutineRun } from '@/composables/useRoutineRuns'
import { computed, onMounted, ref, watch } from 'vue'
import AppButton from '@/components/ui/AppButton.vue'
import { fetchRoutineRuns } from '@/composables/useRoutineRuns'
import { toast } from '@/composables/useToast'
import { useTasks } from '@/features/pipeline/composables/useTasks'
import { errorMessage } from '@/utils/errorMessage'
import { formatCost, formatDateTime } from '@/utils/format'

const props = defineProps<{
  scheduleId: string
}>()

const { tasks, selectTask } = useTasks({ autoStart: false })

const runs = ref<RoutineRun[]>([])
const isLoading = ref(true)
const hasLoaded = ref(false)
const error = ref<string | null>(null)

async function load() {
  if (!hasLoaded.value)
    isLoading.value = true
  error.value = null
  try {
    runs.value = await fetchRoutineRuns(props.scheduleId)
  }
  catch (err) {
    error.value = errorMessage(err, 'Failed to load runs')
  }
  finally {
    isLoading.value = false
    hasLoaded.value = true
  }
}

onMounted(load)

// Re-fetch whenever a task belonging to this routine changes stage or run
// status, so a running/failed/completed job reflects here without polling.
const routineTasksSignature = computed(() =>
  tasks.value
    .filter(t => t.routineId === props.scheduleId)
    .map(t => `${t.id}:${t.currentStage}:${t.latestStageRunStatus}`)
    .join(','),
)
watch(routineTasksSignature, load)

function formatRunDuration(run: RoutineRun): string | null {
  if (!run.startedAt || !run.endedAt)
    return null
  const seconds = Math.round((Date.parse(run.endedAt) - Date.parse(run.startedAt)) / 1000)
  if (!Number.isFinite(seconds) || seconds < 0)
    return null
  return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
}

async function open(run: RoutineRun) {
  const existing = tasks.value.find(t => t.id === run.taskId)
  if (existing) {
    selectTask(existing)
    return
  }
  const res = await fetch(`/api/tasks/${run.taskId}`)
  if (res.ok) {
    selectTask(await res.json())
    return
  }
  toast.error(`Could not open run: HTTP ${res.status}`)
}
</script>

<template>
  <div class="routine-runs">
    <p v-if="isLoading" class="text-xs text-fg-faint">
      Loading runs…
    </p>
    <p v-else-if="error" role="alert" class="text-xs text-danger-text">
      {{ error }}
    </p>
    <p v-else-if="runs.length === 0" class="text-xs text-fg-faint">
      No runs yet
    </p>
    <ul v-else class="flex flex-col divide-y divide-line text-xs">
      <li
        v-for="run in runs"
        :key="run.taskId"
        :data-testid="`routine-run-${run.taskId}`"
        class="flex flex-wrap items-center gap-x-3 gap-y-1 py-1.5"
      >
        <span class="text-fg font-medium">{{ run.title }}</span>
        <span class="px-1.5 py-0.5 rounded-full bg-raised text-fg-mute">{{ run.status }}</span>
        <span v-if="run.summary" class="text-fg-mute basis-full">{{ run.summary }}</span>
        <span class="text-fg-faint">{{ formatCost(run.costCents / 100) }}</span>
        <span class="text-fg-faint">{{ formatDateTime(run.startedAt) }}</span>
        <span v-if="formatRunDuration(run)" class="text-fg-faint">{{ formatRunDuration(run) }}</span>
        <AppButton variant="ghost" size="sm" @click="open(run)">
          Open
        </AppButton>
      </li>
    </ul>
  </div>
</template>
