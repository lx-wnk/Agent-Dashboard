<script setup lang="ts">
import type { ScheduleView } from '../composables/useSchedules'
import { ref, watch } from 'vue'
import { deleteSchedule, runScheduleNow, updateSchedule, useSchedules } from '../composables/useSchedules'
import { toast } from '../composables/useToast'
import { formatDateTime } from '../utils/format'
import ScheduleForm from './ScheduleForm.vue'
import AppButton from './ui/AppButton.vue'
import AppModal from './ui/AppModal.vue'

const { schedules, isLoading, error } = useSchedules()

// Surface async load failures as toasts; the view keeps its empty/loading state.
watch(error, (msg) => {
  if (msg)
    toast.error(msg)
})

const showForm = ref(false)
const editTarget = ref<ScheduleView | null>(null)

function openCreate() {
  editTarget.value = null
  showForm.value = true
}

function openEdit(s: ScheduleView) {
  editTarget.value = s
  showForm.value = true
}

function onSaved() {
  showForm.value = false
  editTarget.value = null
}

function onCancel() {
  showForm.value = false
  editTarget.value = null
}

async function onToggleEnabled(s: ScheduleView) {
  try {
    await updateSchedule(s.id, { enabled: !s.enabled })
  }
  catch (err) {
    toast.error((err as Error).message)
  }
}

async function onRunNow(s: ScheduleView) {
  try {
    await runScheduleNow(s.id)
  }
  catch (err) {
    toast.error((err as Error).message)
  }
}

async function onDelete(s: ScheduleView) {
  // eslint-disable-next-line no-alert -- native confirm guard for a destructive delete
  if (!globalThis.confirm(`Delete schedule "${s.name}"?`))
    return
  try {
    await deleteSchedule(s.id)
  }
  catch (err) {
    toast.error((err as Error).message)
  }
}
</script>

<template>
  <div class="flex flex-col gap-4 p-4 max-w-5xl mx-auto">
    <div class="flex items-center justify-between">
      <h1 class="text-lg font-semibold text-fg">
        Schedules
      </h1>
      <AppButton variant="primary" @click="openCreate">
        + New Schedule
      </AppButton>
    </div>

    <div v-if="isLoading" class="text-fg-faint text-sm">
      Loading schedules…
    </div>

    <div v-else-if="schedules.length === 0" class="text-fg-faint text-sm">
      No schedules yet. Create one to get started.
    </div>

    <div v-else class="flex flex-col gap-2">
      <div
        v-for="s in schedules"
        :key="s.id"
        class="bg-card border border-line rounded-lg px-4 py-3 flex flex-col sm:flex-row sm:items-center gap-3"
      >
        <div class="flex-1 min-w-0">
          <div class="flex items-center gap-2">
            <span class="font-semibold text-fg truncate">{{ s.name }}</span>
            <span
              class="text-[11px] px-1.5 py-0.5 rounded-full font-medium shrink-0"
              :class="s.enabled ? 'bg-green-900/40 text-green-400' : 'bg-raised text-fg-faint'"
            >
              {{ s.enabled ? 'enabled' : 'disabled' }}
            </span>
          </div>
          <div class="text-fg-mute text-sm mt-0.5">
            {{ s.human }}
          </div>
          <div class="flex gap-4 mt-1 text-xs text-fg-faint flex-wrap">
            <span class="font-mono">{{ s.cronExpr }}</span>
            <span>Next: {{ formatDateTime(s.nextRunAt) }}</span>
            <span v-if="s.lastRunAt">Last: {{ formatDateTime(s.lastRunAt) }}</span>
          </div>
        </div>

        <div class="flex items-center gap-2 shrink-0 flex-wrap">
          <button
            type="button"
            class="text-xs px-2 py-1 rounded border border-line text-fg-mute hover:text-fg hover:border-fg-mute transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
            :aria-label="s.enabled ? 'Disable schedule' : 'Enable schedule'"
            @click="onToggleEnabled(s)"
          >
            {{ s.enabled ? 'Disable' : 'Enable' }}
          </button>
          <AppButton variant="secondary" size="sm" @click="onRunNow(s)">
            Run Now
          </AppButton>
          <AppButton variant="ghost" size="sm" @click="openEdit(s)">
            Edit
          </AppButton>
          <AppButton variant="danger" size="sm" @click="onDelete(s)">
            Delete
          </AppButton>
        </div>
      </div>
    </div>

    <AppModal :open="showForm" labelled-by="schedule-form-title" @close="onCancel">
      <div class="bg-raised px-4 py-3 flex items-center justify-between border-b border-line shrink-0">
        <h2 id="schedule-form-title" class="text-sm font-semibold text-fg">
          {{ editTarget ? 'Edit Schedule' : 'New Schedule' }}
        </h2>
        <button
          type="button"
          class="text-fg-faint hover:text-fg rounded p-1 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
          aria-label="Close"
          @click="onCancel"
        >
          ✕
        </button>
      </div>
      <div class="flex-1 overflow-y-auto p-4">
        <ScheduleForm
          :schedule="editTarget"
          @saved="onSaved"
          @cancel="onCancel"
        />
      </div>
    </AppModal>
  </div>
</template>
