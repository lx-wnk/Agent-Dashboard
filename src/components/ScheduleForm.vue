<script setup lang="ts">
import type { CreateScheduleBody, SchedulePreview, ScheduleView, UpdateScheduleBody } from '../composables/useSchedules'
import { ref, watch } from 'vue'
import { createSchedule, previewSchedule, updateSchedule } from '../composables/useSchedules'
import AppButton from './ui/AppButton.vue'
import AppInput from './ui/AppInput.vue'
import AppSelect from './ui/AppSelect.vue'

const props = defineProps<{
  schedule?: ScheduleView | null
}>()

const emit = defineEmits<{
  saved: [schedule: ScheduleView]
  cancel: []
}>()

const name = ref(props.schedule?.name ?? '')
const nlText = ref(props.schedule?.nlText ?? '')
const timezone = ref(props.schedule?.timezone ?? 'UTC')
const catchup = ref<'none' | 'once'>(props.schedule?.catchup ? 'once' : 'none')
const slugPrefix = ref(props.schedule?.slugPrefix ?? '')
const title = ref(props.schedule?.title ?? '')
const description = ref(props.schedule?.description ?? '')
const cwd = ref(props.schedule?.cwd ?? '')
const priority = ref(props.schedule?.priority ?? 'medium')
const maxIterations = ref(props.schedule?.maxIterations ?? 20)
const permissionTemplate = ref(props.schedule?.permissionTemplate ?? '')

const CATCHUP_OPTIONS = [
  { value: 'none', label: 'None' },
  { value: 'once', label: 'Once' },
]

const PRIORITY_OPTIONS = [
  { value: 'high', label: 'High' },
  { value: 'medium', label: 'Medium' },
  { value: 'low', label: 'Low' },
]

const preview = ref<SchedulePreview | null>(null)
const previewError = ref<string | null>(null)
const submitError = ref<string | null>(null)
const isSaving = ref(false)

let debounceTimer: ReturnType<typeof setTimeout> | null = null

function schedulePreview() {
  previewError.value = null
  preview.value = null
  if (debounceTimer)
    clearTimeout(debounceTimer)
  const phrase = nlText.value.trim()
  if (!phrase)
    return
  debounceTimer = setTimeout(async () => {
    try {
      preview.value = await previewSchedule({ nlText: phrase, timezone: timezone.value || undefined })
      previewError.value = null
    }
    catch {
      previewError.value = 'Couldn\'t understand that phrase'
      preview.value = null
    }
  }, 400)
}

// Both the phrase and the timezone affect the parsed cron + next runs.
watch([nlText, timezone], schedulePreview)

function formatNextRun(iso: string): string {
  try {
    return new Date(iso).toLocaleString()
  }
  catch {
    return iso
  }
}

async function onSubmit() {
  submitError.value = null
  isSaving.value = true
  try {
    const body: CreateScheduleBody = {
      name: name.value.trim(),
      nlText: nlText.value.trim() || undefined,
      timezone: timezone.value || undefined,
      catchup: catchup.value === 'once',
      slugPrefix: slugPrefix.value.trim(),
      title: title.value.trim(),
      description: description.value.trim() || undefined,
      cwd: cwd.value.trim(),
      priority: priority.value || undefined,
      maxIterations: maxIterations.value || undefined,
      permissionTemplate: permissionTemplate.value.trim() || undefined,
    }

    let saved: ScheduleView
    if (props.schedule) {
      saved = await updateSchedule(props.schedule.id, body as UpdateScheduleBody)
    }
    else {
      saved = await createSchedule(body)
    }
    emit('saved', saved)
  }
  catch (err) {
    submitError.value = (err as Error).message
  }
  finally {
    isSaving.value = false
  }
}
</script>

<template>
  <form class="flex flex-col gap-4" @submit.prevent="onSubmit">
    <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
      <AppInput
        v-model="name"
        label="Schedule Name"
        placeholder="Daily standup"
        required
      />
      <AppInput
        v-model="slugPrefix"
        label="Slug Prefix"
        placeholder="sched-daily"
        required
      />
    </div>

    <div class="flex flex-col gap-1">
      <AppInput
        v-model="nlText"
        label="Schedule Phrase"
        placeholder="e.g. &quot;every weekday at 9am&quot;"
      />
      <div
        v-if="previewError"
        role="status"
        aria-live="polite"
        class="text-danger-text text-sm"
      >
        {{ previewError }}
      </div>
      <div
        v-if="preview"
        class="rounded-md bg-raised border border-line p-3 text-sm flex flex-col gap-1"
        aria-live="polite"
      >
        <div class="flex items-center gap-2">
          <span class="text-fg-faint font-mono text-xs">{{ preview.cronExpr }}</span>
          <span class="text-fg-mute">—</span>
          <span class="text-fg font-medium">{{ preview.human }}</span>
        </div>
        <div class="text-fg-faint text-xs mt-1">
          Next 5 runs:
        </div>
        <ul class="flex flex-col gap-0.5">
          <li
            v-for="(run, i) in preview.nextRuns"
            :key="i"
            class="text-fg-mute text-xs font-mono"
          >
            {{ formatNextRun(run) }}
          </li>
        </ul>
      </div>
    </div>

    <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
      <AppInput
        v-model="timezone"
        label="Timezone"
        placeholder="UTC"
      />
      <div class="flex flex-col gap-1">
        <label for="schedule-catchup" class="text-sm font-medium text-fg-soft">Catchup</label>
        <AppSelect
          id="schedule-catchup"
          v-model="catchup"
          :options="CATCHUP_OPTIONS"
        />
      </div>
    </div>

    <AppInput
      v-model="title"
      label="Task Title"
      placeholder="Run daily standup task"
      required
    />

    <AppInput
      v-model="description"
      type="textarea"
      label="Description"
      placeholder="Optional task description"
      :rows="2"
      resize="y"
    />

    <AppInput
      v-model="cwd"
      label="Working Directory"
      placeholder="/home/user/project"
      required
    />

    <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
      <div class="flex flex-col gap-1">
        <label for="schedule-priority" class="text-sm font-medium text-fg-soft">Priority</label>
        <AppSelect
          id="schedule-priority"
          v-model="priority"
          :options="PRIORITY_OPTIONS"
        />
      </div>
      <div class="flex flex-col gap-1">
        <label class="text-sm font-medium text-fg-soft">Max Iterations</label>
        <input
          v-model.number="maxIterations"
          type="number"
          min="1"
          class="bg-app border border-line rounded-md px-3 py-1.5 text-sm text-fg focus:outline-none focus:ring-2 focus:ring-blue-500/50 focus:border-blue-500"
        >
      </div>
    </div>

    <AppInput
      v-model="permissionTemplate"
      label="Permission Template"
      placeholder="Optional permission template name"
    />

    <div
      v-if="submitError"
      role="alert"
      class="text-danger-text text-sm"
    >
      {{ submitError }}
    </div>

    <div class="flex gap-2 justify-end pt-2">
      <AppButton
        variant="secondary"
        type="button"
        @click="emit('cancel')"
      >
        Cancel
      </AppButton>
      <AppButton
        variant="primary"
        type="submit"
        :disabled="isSaving"
      >
        {{ isSaving ? 'Saving…' : (schedule ? 'Update Schedule' : 'Create Schedule') }}
      </AppButton>
    </div>
  </form>
</template>
