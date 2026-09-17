<script setup lang="ts">
import type { RoutineRun } from '@/composables/useRoutineRuns'
import { computed, onMounted, ref, watch } from 'vue'
import { fetchRoutineRuns } from '@/composables/useRoutineRuns'
import { useTasks } from '@/features/pipeline/composables/useTasks'
import { errorMessage } from '@/utils/errorMessage'
import { formatCost, formatDateTime } from '@/utils/format'

const props = defineProps<{
  scheduleId: string
}>()

const { tasks, selectTask } = useTasks({ autoStart: false })

const runs = ref<RoutineRun[]>([])
const isLoading = ref(true)
const error = ref<string | null>(null)

async function load() {
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
  if (res.ok)
    selectTask(await res.json())
}
</script>

<template>
  <div class="routine-runs">
    <p v-if="isLoading">
      Loading runs…
    </p>
    <p v-else-if="error" role="alert">
      {{ error }}
    </p>
    <p v-else-if="runs.length === 0">
      No runs yet
    </p>
    <ul v-else>
      <li v-for="run in runs" :key="run.taskId" :data-testid="`routine-run-${run.taskId}`">
        <span>{{ run.title }}</span>
        <span>{{ run.status }}</span>
        <span v-if="run.summary">{{ run.summary }}</span>
        <span>{{ formatCost(run.costCents / 100) }}</span>
        <span>{{ formatDateTime(run.startedAt) }}</span>
        <span v-if="formatRunDuration(run)">{{ formatRunDuration(run) }}</span>
        <button type="button" @click="open(run)">
          Open
        </button>
      </li>
    </ul>
  </div>
</template>
