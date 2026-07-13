<script setup lang="ts">
import { onMounted, ref } from 'vue'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'
import { toast } from '@/composables/useToast'
import { errorMessage } from '@/utils/errorMessage'
import { STAGE_LABELS } from '@/utils/stageLabels'

interface SystemPrompt {
  id: string
  scope: string
  stage: string | null
  content: string
  priority: number
  created_at: string
  updated_at: string
  created_by?: string | null
}

interface PromptForm {
  stage: string
  priority: number
  content: string
}

const STAGES = ['', 'implementation', 'self_review', 'finalization'] as const

const prompts = ref<SystemPrompt[]>([])
const loading = ref(true)
const saving = ref(false)

const showDialog = ref(false)
const editing = ref<SystemPrompt | null>(null)
const confirmDeleteId = ref<string | null>(null)

const form = ref<PromptForm>({ stage: '', priority: 0, content: '' })

async function fetchPrompts() {
  loading.value = true
  try {
    const res = await fetch('/api/settings/system-prompts')
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    prompts.value = await res.json()
  }
  catch (e) {
    toast.error(errorMessage(e, 'Failed to load'))
  }
  finally {
    loading.value = false
  }
}

function openCreate() {
  editing.value = null
  form.value = { stage: '', priority: 0, content: '' }
  showDialog.value = true
}

function openEdit(p: SystemPrompt) {
  editing.value = p
  form.value = { stage: p.stage ?? '', priority: p.priority, content: p.content }
  showDialog.value = true
}

function closeDialog() {
  showDialog.value = false
  editing.value = null
}

async function save() {
  if (saving.value)
    return
  if (!form.value.content.trim()) {
    toast.error('Content is required')
    return
  }

  saving.value = true
  try {
    const body = editing.value
      ? {
          stage: form.value.stage,
          priority: form.value.priority,
          content: form.value.content,
        }
      : {
          scope: 'global',
          stage: form.value.stage,
          priority: form.value.priority,
          content: form.value.content,
        }
    const url = editing.value
      ? `/api/settings/system-prompts/${editing.value.id}`
      : '/api/settings/system-prompts'
    const method = editing.value ? 'PUT' : 'POST'
    const res = await fetch(url, {
      method,
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
    if (!res.ok) {
      const data = await res.json().catch(() => ({}))
      throw new Error((data as { error?: string }).error ?? `HTTP ${res.status}`)
    }
    closeDialog()
    await fetchPrompts()
  }
  catch (e) {
    toast.error(errorMessage(e, 'Save failed'))
  }
  finally {
    saving.value = false
  }
}

async function deletePrompt(id: string) {
  confirmDeleteId.value = null
  try {
    const res = await fetch(`/api/settings/system-prompts/${id}`, { method: 'DELETE' })
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    await fetchPrompts()
  }
  catch (e) {
    toast.error(errorMessage(e, 'Delete failed'))
  }
}

function stageLabel(stage: string | null) {
  if (!stage)
    return 'All stages'
  return STAGE_LABELS[stage as keyof typeof STAGE_LABELS] ?? stage.replace(/_/g, ' ')
}

onMounted(fetchPrompts)
</script>

<template>
  <div>
    <div class="flex items-start justify-between gap-3 mb-4">
      <div class="flex-1">
        <h3 class="text-[17px] font-bold text-fg mb-1">
          Custom System Prompts
        </h3>
        <p class="text-xs text-fg-mute">
          Prepend custom instructions to the built-in system prompt for pipeline stage agents. Higher priority is applied first.
        </p>
      </div>
      <AppButton variant="info" @click="openCreate">
        + Add Prompt
      </AppButton>
    </div>

    <div v-if="loading" class="text-center py-12 text-fg-mute text-sm">
      Loading...
    </div>

    <div v-else-if="prompts.length === 0" class="text-center py-8 text-fg-mute text-sm">
      No custom system prompts configured yet.
    </div>

    <table v-else class="w-full border-collapse text-[13px]">
      <thead>
        <tr>
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            Stage
          </th>
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            Priority
          </th>
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            Preview
          </th>
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            Actions
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="p in prompts" :key="p.id">
          <td class="px-3 py-2.5 border-b border-line text-fg whitespace-nowrap font-medium capitalize">
            {{ stageLabel(p.stage) }}
          </td>
          <td class="px-3 py-2.5 border-b border-line text-fg-mute">
            {{ p.priority }}
          </td>
          <td class="px-3 py-2.5 border-b border-line text-fg-mute font-mono text-xs max-w-[320px] truncate">
            {{ p.content.slice(0, 100) }}{{ p.content.length > 100 ? '…' : '' }}
          </td>
          <td class="px-3 py-2.5 border-b border-line whitespace-nowrap">
            <button
              type="button"
              class="bg-transparent border-none text-fg-mute cursor-pointer text-sm px-2 py-1 rounded hover:bg-raised hover:text-fg-soft mr-1"
              @click="openEdit(p)"
            >
              Edit
            </button>
            <template v-if="confirmDeleteId === p.id">
              <AppButton variant="danger" size="sm" class="mr-1" @click="deletePrompt(p.id)">
                Confirm
              </AppButton>
              <AppButton variant="secondary" size="sm" @click="confirmDeleteId = null">
                Cancel
              </AppButton>
            </template>
            <button
              v-else
              type="button"
              class="bg-transparent border-none text-fg-mute cursor-pointer text-sm px-2 py-1 rounded hover:bg-red-50 dark:hover:bg-red-950/30 hover:text-red-600 dark:hover:text-red-400"
              @click="confirmDeleteId = p.id"
            >
              Delete
            </button>
          </td>
        </tr>
      </tbody>
    </table>

    <!-- Create / Edit dialog -->
    <AppModal :open="showDialog" @close="closeDialog">
      <header class="shrink-0 flex justify-between items-center px-5 py-4 border-b border-line">
        <h2 class="text-lg font-semibold text-fg">
          {{ editing ? 'Edit System Prompt' : 'New System Prompt' }}
        </h2>
        <button
          type="button"
          class="bg-transparent border-none text-fg-mute text-2xl cursor-pointer px-1 leading-none hover:text-fg"
          @click="closeDialog"
        >
          &times;
        </button>
      </header>
      <form class="flex-1 min-h-0 overflow-y-auto p-5 flex flex-col gap-4" @submit.prevent="save">
        <div class="flex flex-col gap-1">
          <label for="sp-stage" class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">
            Stage (blank = all stages)
          </label>
          <select
            id="sp-stage"
            v-model="form.stage"
            class="w-full bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
          >
            <option v-for="s in STAGES" :key="s" :value="s">
              {{ s === '' ? 'All stages' : s.replace(/_/g, ' ') }}
            </option>
          </select>
        </div>
        <div class="flex flex-col gap-1">
          <label for="sp-priority" class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">
            Priority (higher = applied first)
          </label>
          <input
            id="sp-priority"
            v-model.number="form.priority"
            type="number"
            class="w-full bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
          >
        </div>
        <div class="flex flex-col gap-1">
          <label for="sp-content" class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">
            Content
          </label>
          <textarea
            id="sp-content"
            v-model="form.content"
            rows="8"
            placeholder="Enter custom system prompt text…"
            class="w-full bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent font-mono resize-y"
          />
        </div>
      </form>
      <footer class="shrink-0 flex justify-end gap-2 px-5 py-3 border-t border-line">
        <AppButton variant="secondary" @click="closeDialog">
          Cancel
        </AppButton>
        <AppButton variant="info" :disabled="saving || !form.content.trim()" @click="save">
          {{ saving ? 'Saving…' : (editing ? 'Update' : 'Create') }}
        </AppButton>
      </footer>
    </AppModal>
  </div>
</template>
