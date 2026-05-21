<script setup lang="ts">
import type { Spawner } from '../types'
import { ref } from 'vue'
import {
  createSpawner,
  deleteSpawner,
  updateSpawner,
  useSpawners,
} from '../composables/useSpawners'
import { isAllowedSpawnerCommand } from '../utils/validation'
import AppButton from './ui/AppButton.vue'

const { spawners, isLoading, error, refetch } = useSpawners()

// ── Form state ──────────────────────────────────────────────────────────────
interface SpawnerFormState {
  name: string
  slug: string
  command: string
  argsRaw: string // newline-separated
  modelOverride: string
  description: string
  envRows: { key: string, value: string, _k: number }[]
}

let _envKey = 0

const editingSpawner = ref<Spawner | null>(null)
const isCreating = ref(false)
const formVisible = ref(false)
const formSaving = ref(false)
const formError = ref<string | null>(null)
const form = ref<SpawnerFormState>({
  name: '',
  slug: '',
  command: 'claude',
  argsRaw: '',
  modelOverride: '',
  description: '',
  envRows: [],
})

function emptyForm(): SpawnerFormState {
  return { name: '', slug: '', command: 'claude', argsRaw: '', modelOverride: '', description: '', envRows: [] }
}

function openCreate() {
  editingSpawner.value = null
  isCreating.value = true
  form.value = emptyForm()
  formError.value = null
  formVisible.value = true
}

function openEdit(spawner: Spawner) {
  if (spawner.builtIn)
    return
  editingSpawner.value = spawner
  isCreating.value = false
  form.value = {
    name: spawner.name,
    slug: spawner.slug,
    command: spawner.command,
    argsRaw: spawner.args.join('\n'),
    modelOverride: spawner.modelOverride ?? '',
    description: spawner.description ?? '',
    envRows: Object.entries(spawner.env).map(([key, value]) => ({ key, value, _k: _envKey++ })),
  }
  formError.value = null
  formVisible.value = true
}

function closeForm() {
  formVisible.value = false
  editingSpawner.value = null
}

function addEnvRow() {
  form.value.envRows.push({ key: '', value: '', _k: _envKey++ })
}

function removeEnvRow(k: number) {
  form.value.envRows = form.value.envRows.filter(r => r._k !== k)
}

function buildEnvRecord(): Record<string, string> {
  const rec: Record<string, string> = {}
  for (const row of form.value.envRows) {
    const k = row.key.trim()
    if (k)
      rec[k] = row.value
  }
  return rec
}

async function handleSave() {
  formError.value = null
  if (!form.value.name.trim() || !form.value.slug.trim() || !form.value.command.trim()) {
    formError.value = 'Name, slug, and command are required.'
    return
  }
  if (!isAllowedSpawnerCommand(form.value.command.trim())) {
    formError.value = 'Command must be one of: claude, claude-code, npx — or an absolute path not under /tmp or /var/tmp.'
    return
  }
  formSaving.value = true
  try {
    const args = form.value.argsRaw.split('\n').map(s => s.trim()).filter(Boolean)
    const env = buildEnvRecord()
    if (isCreating.value) {
      await createSpawner({
        name: form.value.name.trim(),
        slug: form.value.slug.trim(),
        command: form.value.command.trim(),
        args,
        env,
        modelOverride: form.value.modelOverride.trim() || undefined,
        description: form.value.description.trim() || undefined,
      })
    }
    else if (editingSpawner.value) {
      await updateSpawner(editingSpawner.value.id, {
        name: form.value.name.trim(),
        slug: form.value.slug.trim(),
        command: form.value.command.trim(),
        args,
        env,
        modelOverride: form.value.modelOverride.trim() || undefined,
        description: form.value.description.trim() || undefined,
      })
    }
    await refetch()
    closeForm()
  }
  catch (e) {
    formError.value = (e as Error).message
  }
  finally {
    formSaving.value = false
  }
}

// ── Delete ──────────────────────────────────────────────────────────────────
const confirmDeleteId = ref<string | null>(null)
const deleteError = ref<string | null>(null)

async function handleDelete(id: string) {
  deleteError.value = null
  try {
    await deleteSpawner(id)
    confirmDeleteId.value = null
    await refetch()
    if (editingSpawner.value?.id === id)
      closeForm()
  }
  catch (e) {
    deleteError.value = (e as Error).message
    confirmDeleteId.value = null
  }
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <!-- Header -->
    <div class="flex items-start justify-between gap-3">
      <div>
        <h3 class="text-[17px] font-bold text-slate-900 dark:text-slate-100 mb-1">
          Spawners
        </h3>
        <p class="text-xs text-slate-400 dark:text-slate-600">
          Configure custom Claude CLI invocations. Built-in spawners are read-only. Custom spawners can override the command, args, env, and model for pipeline stage agents.
        </p>
      </div>
      <AppButton variant="info" @click="openCreate">
        + New Spawner
      </AppButton>
    </div>

    <p v-if="error" class="text-xs text-red-600 dark:text-red-400">
      {{ error }}
    </p>
    <p v-if="deleteError" class="text-xs text-red-600 dark:text-red-400">
      {{ deleteError }}
    </p>

    <div v-if="isLoading" class="text-center py-12 text-slate-400 dark:text-slate-600 text-sm">
      Loading spawners...
    </div>

    <!-- Spawner list + form side-by-side when editing -->
    <template v-else>
      <table v-if="!formVisible || spawners.length > 0" class="w-full border-collapse text-[13px]">
        <thead>
          <tr>
            <th class="text-left text-[10px] uppercase tracking-wide text-slate-400 dark:text-slate-600 px-3 py-2 border-b border-slate-200 dark:border-slate-700">
              Name
            </th>
            <th class="text-left text-[10px] uppercase tracking-wide text-slate-400 dark:text-slate-600 px-3 py-2 border-b border-slate-200 dark:border-slate-700">
              Command
            </th>
            <th class="text-left text-[10px] uppercase tracking-wide text-slate-400 dark:text-slate-600 px-3 py-2 border-b border-slate-200 dark:border-slate-700">
              Type
            </th>
            <th class="text-left text-[10px] uppercase tracking-wide text-slate-400 dark:text-slate-600 px-3 py-2 border-b border-slate-200 dark:border-slate-700">
              Actions
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="spawner in spawners" :key="spawner.id">
            <td class="px-3 py-2.5 border-b border-slate-200 dark:border-slate-700">
              <div class="font-semibold text-slate-900 dark:text-slate-100">
                {{ spawner.name }}
              </div>
              <div v-if="spawner.description" class="text-[11px] text-slate-400 dark:text-slate-600 line-clamp-1">
                {{ spawner.description }}
              </div>
            </td>
            <td class="px-3 py-2.5 border-b border-slate-200 dark:border-slate-700 font-mono text-xs text-slate-500 dark:text-slate-400">
              {{ spawner.command }}
              <span v-if="spawner.args.length" class="opacity-60"> {{ spawner.args.join(' ') }}</span>
            </td>
            <td class="px-3 py-2.5 border-b border-slate-200 dark:border-slate-700">
              <span
                class="text-[10px] font-semibold uppercase tracking-wider px-1.5 py-px rounded"
                :class="spawner.builtIn
                  ? 'bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400'
                  : 'bg-blue-50 dark:bg-blue-950/30 text-blue-600 dark:text-blue-400'"
              >{{ spawner.builtIn ? 'Built-in' : 'Custom' }}</span>
            </td>
            <td class="px-3 py-2.5 border-b border-slate-200 dark:border-slate-700 whitespace-nowrap">
              <template v-if="spawner.builtIn">
                <span class="text-xs text-slate-400 dark:text-slate-600">Read-only</span>
              </template>
              <template v-else-if="confirmDeleteId === spawner.id">
                <AppButton variant="danger" size="sm" class="mr-1" @click="handleDelete(spawner.id)">
                  Confirm
                </AppButton>
                <AppButton variant="secondary" size="sm" @click="confirmDeleteId = null">
                  Cancel
                </AppButton>
              </template>
              <template v-else>
                <button
                  type="button"
                  class="bg-transparent border-none text-slate-400 dark:text-slate-600 cursor-pointer text-sm px-2 py-1 rounded hover:bg-blue-50 dark:hover:bg-blue-950/30 hover:text-blue-600 dark:hover:text-blue-400 mr-1"
                  @click="openEdit(spawner)"
                >
                  Edit
                </button>
                <button
                  type="button"
                  class="bg-transparent border-none text-slate-400 dark:text-slate-600 cursor-pointer text-sm px-2 py-1 rounded hover:bg-red-50 dark:hover:bg-red-950/30 hover:text-red-600 dark:hover:text-red-400"
                  @click="confirmDeleteId = spawner.id"
                >
                  Delete
                </button>
              </template>
            </td>
          </tr>
        </tbody>
      </table>

      <div v-if="!formVisible && !spawners.length" class="text-center py-8 text-slate-400 dark:text-slate-600 text-sm">
        No custom spawners yet.
      </div>

      <!-- Create / Edit form -->
      <div v-if="formVisible" class="border border-slate-200 dark:border-slate-700 rounded-lg p-4 flex flex-col gap-3 mt-1">
        <div class="flex items-center justify-between">
          <h4 class="text-sm font-semibold text-slate-900 dark:text-slate-100">
            {{ isCreating ? 'New Spawner' : `Edit: ${editingSpawner?.name}` }}
          </h4>
          <button type="button" class="bg-transparent border-none text-slate-400 dark:text-slate-600 text-lg cursor-pointer px-1 leading-none hover:text-slate-900 dark:hover:text-slate-100" @click="closeForm">
            &times;
          </button>
        </div>

        <div class="grid grid-cols-2 gap-3">
          <div>
            <label class="block text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-1" for="sp-name">Name</label>
            <input
              id="sp-name"
              v-model="form.name"
              type="text"
              class="w-full bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded px-2.5 py-1.5 text-sm text-slate-900 dark:text-slate-100 focus:outline-none focus:border-blue-500"
              placeholder="My Spawner"
            >
          </div>
          <div>
            <label class="block text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-1" for="sp-slug">Slug</label>
            <input
              id="sp-slug"
              v-model="form.slug"
              type="text"
              class="w-full bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded px-2.5 py-1.5 text-sm text-slate-900 dark:text-slate-100 font-mono focus:outline-none focus:border-blue-500"
              placeholder="my-spawner"
            >
          </div>
          <div class="col-span-2">
            <label class="block text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-1" for="sp-command">
              Command <span class="normal-case font-normal">(claude, claude-code, npx, or absolute path not under /tmp)</span>
            </label>
            <input
              id="sp-command"
              v-model="form.command"
              type="text"
              class="w-full bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded px-2.5 py-1.5 text-sm text-slate-900 dark:text-slate-100 font-mono focus:outline-none focus:border-blue-500"
              placeholder="claude"
            >
          </div>
          <div class="col-span-2">
            <label class="block text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-1" for="sp-args">Args (one per line)</label>
            <textarea
              id="sp-args"
              v-model="form.argsRaw"
              rows="3"
              class="w-full bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded px-2.5 py-1.5 text-sm text-slate-900 dark:text-slate-100 font-mono focus:outline-none focus:border-blue-500 resize-none"
              placeholder="--no-color&#10;--dangerously-skip-permissions"
            />
          </div>
          <div>
            <label class="block text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-1" for="sp-model">Model Override (optional)</label>
            <input
              id="sp-model"
              v-model="form.modelOverride"
              type="text"
              class="w-full bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded px-2.5 py-1.5 text-sm text-slate-900 dark:text-slate-100 font-mono focus:outline-none focus:border-blue-500"
              placeholder="claude-opus-4-5"
            >
          </div>
          <div>
            <label class="block text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-1" for="sp-desc">Description (optional)</label>
            <input
              id="sp-desc"
              v-model="form.description"
              type="text"
              class="w-full bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded px-2.5 py-1.5 text-sm text-slate-900 dark:text-slate-100 focus:outline-none focus:border-blue-500"
              placeholder="Short description"
            >
          </div>
        </div>

        <!-- Env key/value table -->
        <div>
          <div class="flex items-center justify-between mb-2">
            <label class="text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600">Environment Variables</label>
            <button type="button" class="text-[11px] px-2 py-0.5 rounded border border-slate-300 dark:border-slate-600 text-slate-500 dark:text-slate-400 hover:bg-slate-50 dark:hover:bg-slate-800" @click="addEnvRow">
              + Add
            </button>
          </div>
          <div v-if="form.envRows.length === 0" class="text-xs text-slate-400 dark:text-slate-600">
            No environment variables set.
          </div>
          <div v-else class="flex flex-col gap-1.5">
            <div v-for="row in form.envRows" :key="row._k" class="flex gap-2 items-center">
              <input
                v-model="row.key"
                type="text"
                class="flex-1 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded px-2 py-1 text-xs font-mono text-slate-900 dark:text-slate-100 focus:outline-none focus:border-blue-500"
                placeholder="KEY"
                :aria-label="`Environment variable name ${row._k}`"
              >
              <span class="text-slate-400 text-xs">=</span>
              <input
                v-model="row.value"
                type="text"
                class="flex-[2] bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded px-2 py-1 text-xs font-mono text-slate-900 dark:text-slate-100 focus:outline-none focus:border-blue-500"
                placeholder="value"
                :aria-label="`Environment variable value ${row._k}`"
              >
              <button
                type="button"
                class="bg-transparent border-none text-red-400 hover:text-red-600 cursor-pointer text-sm p-1"
                :aria-label="`Remove env variable ${row.key}`"
                @click="removeEnvRow(row._k)"
              >
                &times;
              </button>
            </div>
          </div>
        </div>

        <p v-if="formError" class="text-xs text-red-600 dark:text-red-400">
          {{ formError }}
        </p>

        <div class="flex gap-2">
          <AppButton variant="info" :disabled="formSaving" @click="handleSave">
            {{ formSaving ? 'Saving…' : (isCreating ? 'Create Spawner' : 'Save Changes') }}
          </AppButton>
          <AppButton variant="secondary" @click="closeForm">
            Cancel
          </AppButton>
        </div>
      </div>
    </template>
  </div>
</template>
