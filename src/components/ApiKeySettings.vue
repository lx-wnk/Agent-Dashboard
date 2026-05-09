<script setup lang="ts">
import type { ApiKey, McpScope } from '../types'
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { useTheme } from '../composables/useTheme'
import { useUser } from '../composables/useUser'
import RemoteSettings from './RemoteSettings.vue'
import AppButton from './ui/AppButton.vue'
import AppModal from './ui/AppModal.vue'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: [] }>()

const { preference: themePref, setTheme } = useTheme()
const { authEnabled } = useUser()

// --- Nav ---
type Section = 'appearance' | 'apiKeys' | 'remotes' | 'permissionPresets'
const activeSection = ref<Section>('appearance')

// --- State ---
const keys = ref<ApiKey[]>([])
const isLoading = ref(true)
const errorMsg = ref('')

// Create key dialog
const showCreateDialog = ref(false)
const newKeyName = ref('')
const newKeyGroup = ref<'viewer' | 'operator' | 'developer' | 'admin'>('viewer')
const isCreating = ref(false)
const createError = ref('')

// Token reveal modal
const revealedToken = ref<string | null>(null)
const copyHint = ref<string | null>(null)

// Revoke confirmation
const confirmRevokeId = ref<string | null>(null)

// Permission presets
const presets = ref<{ cwd: string, count: number }[]>([])
const presetsLoading = ref(false)
const presetsError = ref<string | null>(null)
const confirmResetCwd = ref<string | null>(null)

// --- Group → scopes mapping ---
const GROUP_SCOPES: Record<string, McpScope[]> = {
  viewer: ['tasks:read'],
  operator: ['tasks:read', 'pipeline:control'],
  developer: ['tasks:read', 'tasks:write', 'pipeline:control'],
  admin: ['tasks:read', 'tasks:write', 'pipeline:control', 'keys:manage'],
}

// --- Load keys ---
async function loadKeys() {
  isLoading.value = true
  errorMsg.value = ''
  try {
    const res = await fetch('/api/settings/api-keys')
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    keys.value = await res.json()
  }
  catch (e) {
    errorMsg.value = (e as Error).message
  }
  finally {
    isLoading.value = false
  }
}

watch(() => props.open, (val) => {
  if (val)
    loadKeys()
})

// --- Permission presets ---
async function loadPresets() {
  presetsLoading.value = true
  presetsError.value = null
  try {
    const res = await fetch('/api/settings/permission-presets')
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    presets.value = await res.json()
  }
  catch (e) {
    presetsError.value = e instanceof Error ? e.message : 'Failed to load'
  }
  finally {
    presetsLoading.value = false
  }
}

async function resetPresets(cwd: string) {
  try {
    const res = await fetch('/api/settings/permission-presets', {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ cwd }),
    })
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    confirmResetCwd.value = null
    await loadPresets()
  }
  catch (e) {
    presetsError.value = e instanceof Error ? e.message : 'Failed to reset'
  }
}

watch(activeSection, (val) => {
  if (val === 'permissionPresets')
    loadPresets()
})

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    if (showCreateDialog.value || confirmRevokeId.value || confirmResetCwd.value) {
      showCreateDialog.value = false
      confirmRevokeId.value = null
      confirmResetCwd.value = null
    }
    else {
      emit('close')
    }
  }
}
onMounted(() => window.addEventListener('keydown', onKeydown))
onUnmounted(() => window.removeEventListener('keydown', onKeydown))

// --- Revoke key ---
async function revokeKey(key: ApiKey) {
  confirmRevokeId.value = null

  // Optimistic update
  key.active = false

  try {
    const res = await fetch(`/api/settings/api-keys/${key.id}`, { method: 'DELETE' })
    if (!res.ok) {
      // Rollback on failure
      key.active = true
      errorMsg.value = `Failed to revoke key: HTTP ${res.status}`
    }
  }
  catch (e) {
    key.active = true
    errorMsg.value = (e as Error).message
  }
}

// --- Create key ---
function openCreateDialog() {
  newKeyName.value = ''
  newKeyGroup.value = 'viewer'
  createError.value = ''
  showCreateDialog.value = true
}

function closeCreateDialog() {
  showCreateDialog.value = false
}

async function handleCreate() {
  if (isCreating.value)
    return
  createError.value = ''

  if (!newKeyName.value.trim()) {
    createError.value = 'Name is required'
    return
  }

  isCreating.value = true
  try {
    const res = await fetch('/api/settings/api-keys', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        name: newKeyName.value.trim(),
        scopes: GROUP_SCOPES[newKeyGroup.value],
      }),
    })
    if (!res.ok) {
      const body = await res.json().catch(() => ({}))
      throw new Error(body.error ?? `HTTP ${res.status}`)
    }
    const data = await res.json() as { key: ApiKey, token: string }
    keys.value.unshift(data.key)
    revealedToken.value = data.token
    closeCreateDialog()
  }
  catch (e) {
    createError.value = (e as Error).message
  }
  finally {
    isCreating.value = false
  }
}

// --- Copy token ---
async function copyToken() {
  if (!revealedToken.value)
    return
  try {
    await navigator.clipboard.writeText(revealedToken.value)
    copyHint.value = revealedToken.value
  }
  catch {
    copyHint.value = '__error__'
  }
  setTimeout(() => {
    copyHint.value = null
  }, 2000)
}

function dismissReveal() {
  revealedToken.value = null
  copyHint.value = null
}

// --- Helpers ---
function formatDate(iso: string | null) {
  if (!iso)
    return '—'
  return new Date(iso).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
}

// --- Historical data import ---
const importStatus = ref('')

async function startImport() {
  importStatus.value = 'Starting…'
  await fetch('/api/history/import', { method: 'POST' })
  const es = new EventSource('/api/history/import/status')
  es.onmessage = (ev) => {
    const p = JSON.parse(ev.data) as { total: number, processed: number, done: boolean }
    importStatus.value = `${p.processed}/${p.total} processed`
    if (p.done) {
      importStatus.value = `Import complete — ${p.processed} sessions processed`
      es.close()
    }
  }
}
</script>

<template>
  <AppModal :open="open" @close="emit('close')">
    <div class="bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 shadow-[0_8px_40px_rgba(0,0,0,0.5)] w-full max-w-[975px] h-[700px] max-h-[85vh] flex overflow-hidden">
      <!-- ── Sidebar ──────────────────────────────── -->
      <nav class="w-[260px] flex-shrink-0 bg-slate-50 dark:bg-slate-950 border-r border-slate-200 dark:border-slate-700 px-3 py-5 flex flex-col">
        <div class="flex items-center justify-between px-1 pb-1 mb-2">
          <h2 class="text-base font-bold text-slate-900 dark:text-slate-100">
            Settings
          </h2>
          <button type="button" class="bg-transparent border-none text-slate-400 dark:text-slate-600 text-2xl cursor-pointer px-1 leading-none hover:text-slate-900 dark:hover:text-slate-100" @click="emit('close')">
            &times;
          </button>
        </div>
        <ul class="list-none p-0 m-0 flex flex-col gap-0.5">
          <li>
            <button
              type="button"
              class="w-full flex items-center gap-2 px-2.5 py-1.5 rounded-md border-none font-sans text-[13px] cursor-pointer text-left transition-colors"
              :class="activeSection === 'appearance'
                ? 'bg-slate-200 dark:bg-slate-800 text-slate-900 dark:text-slate-100 font-semibold'
                : 'bg-transparent text-slate-500 dark:text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800/50 hover:text-slate-900 dark:hover:text-slate-100'"
              @click="activeSection = 'appearance'"
            >
              <span class="text-sm flex-shrink-0">◑</span> Appearance
            </button>
          </li>
          <li>
            <button
              type="button"
              class="w-full flex items-center gap-2 px-2.5 py-1.5 rounded-md border-none font-sans text-[13px] cursor-pointer text-left transition-colors"
              :class="activeSection === 'apiKeys'
                ? 'bg-slate-200 dark:bg-slate-800 text-slate-900 dark:text-slate-100 font-semibold'
                : 'bg-transparent text-slate-500 dark:text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800/50 hover:text-slate-900 dark:hover:text-slate-100'"
              @click="activeSection = 'apiKeys'"
            >
              <span class="text-sm flex-shrink-0">⬡</span> API Keys
            </button>
          </li>
          <li v-if="authEnabled">
            <button
              type="button"
              class="w-full flex items-center gap-2 px-2.5 py-1.5 rounded-md border-none font-sans text-[13px] cursor-pointer text-left transition-colors"
              :class="activeSection === 'remotes'
                ? 'bg-slate-200 dark:bg-slate-800 text-slate-900 dark:text-slate-100 font-semibold'
                : 'bg-transparent text-slate-500 dark:text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800/50 hover:text-slate-900 dark:hover:text-slate-100'"
              @click="activeSection = 'remotes'"
            >
              <span class="text-sm flex-shrink-0">⌂</span> Meine Remotes
            </button>
          </li>
          <li>
            <button
              type="button"
              class="w-full flex items-center gap-2 px-2.5 py-1.5 rounded-md border-none font-sans text-[13px] cursor-pointer text-left transition-colors"
              :class="activeSection === 'permissionPresets'
                ? 'bg-slate-200 dark:bg-slate-800 text-slate-900 dark:text-slate-100 font-semibold'
                : 'bg-transparent text-slate-500 dark:text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800/50 hover:text-slate-900 dark:hover:text-slate-100'"
              @click="activeSection = 'permissionPresets'"
            >
              <span class="text-sm flex-shrink-0">⚿</span> Permissions
            </button>
          </li>
        </ul>
        <div class="mt-auto pt-3 border-t border-slate-200 dark:border-slate-700">
          <a
            class="text-xs text-slate-400 dark:text-slate-600 no-underline block px-1.5 py-1 rounded hover:text-slate-500 dark:hover:text-slate-400 transition-colors"
            href="https://github.com/lx-wnk/Agent-Dashboard/issues/new/choose"
            target="_blank"
            rel="noopener noreferrer"
          >Report an issue</a>
        </div>
      </nav>

      <!-- ── Content ─────────────────────────────── -->
      <div class="flex-1 overflow-y-auto px-7 py-7">
        <!-- Appearance -->
        <section v-if="activeSection === 'appearance'">
          <h3 class="text-[17px] font-bold text-slate-900 dark:text-slate-100 mb-1">
            Themes
          </h3>
          <p class="text-xs text-slate-400 dark:text-slate-600 mb-5">
            Choose your preferred color scheme.
          </p>
          <div class="flex gap-3.5 flex-wrap">
            <button
              v-for="opt in ([
                { value: 'light', label: 'Light Mode' },
                { value: 'dark', label: 'Dark Mode' },
                { value: 'system', label: 'System' },
              ] as const)"
              :key="opt.value"
              type="button"
              class="w-40 border-2 rounded-lg overflow-hidden cursor-pointer bg-transparent p-0 transition-all font-sans"
              :class="themePref === opt.value
                ? 'border-blue-500 shadow-[0_0_0_3px_rgba(59,130,246,0.2)]'
                : 'border-slate-200 dark:border-slate-700 hover:border-blue-400'"
              @click="setTheme(opt.value)"
            >
              <div
                class="h-[100px] flex flex-col"
                :class="{
                  'bg-[#f0f4f8]': opt.value === 'light',
                  'bg-[#1a2235]': opt.value === 'dark',
                  'bg-[linear-gradient(135deg,#f0f4f8_50%,#1a2235_50%)]': opt.value === 'system',
                }"
              >
                <div
                  class="h-3.5 mx-2 mt-2 mb-1.5 rounded-sm"
                  :class="{
                    'bg-[#cbd5e1]': opt.value === 'light',
                    'bg-[#2d3f5a]': opt.value === 'dark',
                    'bg-[color-mix(in_srgb,#cbd5e1_50%,#2d3f5a)]': opt.value === 'system',
                  }"
                />
                <div class="flex flex-1 gap-1.5 px-2 pb-2">
                  <div
                    class="w-7 rounded-sm"
                    :class="{
                      'bg-[#cbd5e1]': opt.value === 'light',
                      'bg-[#243248]': opt.value === 'dark',
                      'bg-[color-mix(in_srgb,#cbd5e1_50%,#243248)]': opt.value === 'system',
                    }"
                  />
                  <div class="flex-1 flex flex-col gap-1 justify-center">
                    <div
                      class="h-1.5 rounded-sm"
                      :class="{
                        'bg-[#cbd5e1]': opt.value === 'light',
                        'bg-[#2d3f5a]': opt.value === 'dark',
                        'bg-[color-mix(in_srgb,#cbd5e1_50%,#2d3f5a)]': opt.value === 'system',
                      }"
                    />
                    <div
                      class="h-1.5 w-3/5 rounded-sm"
                      :class="{
                        'bg-[#cbd5e1]': opt.value === 'light',
                        'bg-[#2d3f5a]': opt.value === 'dark',
                        'bg-[color-mix(in_srgb,#cbd5e1_50%,#2d3f5a)]': opt.value === 'system',
                      }"
                    />
                    <div
                      class="h-1.5 rounded-sm"
                      :class="{
                        'bg-[#cbd5e1]': opt.value === 'light',
                        'bg-[#2d3f5a]': opt.value === 'dark',
                        'bg-[color-mix(in_srgb,#cbd5e1_50%,#2d3f5a)]': opt.value === 'system',
                      }"
                    />
                  </div>
                </div>
              </div>
              <div class="px-2.5 py-2 text-xs font-medium text-slate-600 dark:text-slate-400 border-t border-slate-200 dark:border-slate-700 flex items-center gap-1.5 bg-white dark:bg-slate-900">
                <span class="w-3.5 text-blue-500 font-bold text-[13px]">{{ themePref === opt.value ? '✓' : '' }}</span>
                {{ opt.label }}
              </div>
            </button>
          </div>
        </section>

        <!-- API Keys -->
        <section v-else-if="activeSection === 'apiKeys'">
          <div class="flex items-start justify-between gap-3 mb-4">
            <div class="flex-1">
              <h3 class="text-[17px] font-bold text-slate-900 dark:text-slate-100 mb-1">
                API Keys
              </h3>
              <p class="text-xs text-slate-400 dark:text-slate-600">
                Manage MCP API keys for external access to this dashboard.
              </p>
            </div>
            <AppButton variant="info" @click="openCreateDialog">
              + Add Key
            </AppButton>
          </div>
          <p v-if="errorMsg" class="text-xs text-red-600 dark:text-red-400 mb-3">
            {{ errorMsg }}
          </p>
          <div v-if="isLoading" class="text-center py-12 text-slate-400 dark:text-slate-600 text-sm">
            Loading keys...
          </div>
          <div v-else-if="keys.length === 0 && !errorMsg" class="text-center py-8 text-slate-400 dark:text-slate-600 text-sm">
            No API keys yet. Create one to allow MCP clients to connect.
          </div>
          <table v-else class="w-full border-collapse text-[13px]">
            <thead>
              <tr>
                <th class="text-left text-[10px] uppercase tracking-wide text-slate-400 dark:text-slate-600 px-3 py-2 border-b border-slate-200 dark:border-slate-700">
                  Name
                </th>
                <th class="text-left text-[10px] uppercase tracking-wide text-slate-400 dark:text-slate-600 px-3 py-2 border-b border-slate-200 dark:border-slate-700">
                  Scopes
                </th>
                <th class="text-left text-[10px] uppercase tracking-wide text-slate-400 dark:text-slate-600 px-3 py-2 border-b border-slate-200 dark:border-slate-700">
                  Created
                </th>
                <th class="text-left text-[10px] uppercase tracking-wide text-slate-400 dark:text-slate-600 px-3 py-2 border-b border-slate-200 dark:border-slate-700">
                  Last Used
                </th>
                <th class="text-left text-[10px] uppercase tracking-wide text-slate-400 dark:text-slate-600 px-3 py-2 border-b border-slate-200 dark:border-slate-700">
                  Status
                </th>
                <th class="text-left text-[10px] uppercase tracking-wide text-slate-400 dark:text-slate-600 px-3 py-2 border-b border-slate-200 dark:border-slate-700">
                  Actions
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="key in keys" :key="key.id" :class="{ 'opacity-45': !key.active }">
                <td class="px-3 py-2.5 border-b border-slate-200 dark:border-slate-700 text-slate-900 dark:text-slate-100 font-medium whitespace-nowrap">
                  {{ key.name }}
                </td>
                <td class="px-3 py-2.5 border-b border-slate-200 dark:border-slate-700">
                  <div class="flex flex-wrap gap-1">
                    <span v-for="scope in key.scopes" :key="scope" class="text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 bg-slate-100 dark:bg-slate-800 px-1.5 py-px rounded font-mono">{{ scope }}</span>
                  </div>
                </td>
                <td class="px-3 py-2.5 border-b border-slate-200 dark:border-slate-700 text-slate-500 dark:text-slate-400 font-mono text-xs">
                  {{ formatDate(key.createdAt) }}
                </td>
                <td class="px-3 py-2.5 border-b border-slate-200 dark:border-slate-700 text-slate-500 dark:text-slate-400 font-mono text-xs">
                  {{ formatDate(key.lastUsedAt) }}
                </td>
                <td class="px-3 py-2.5 border-b border-slate-200 dark:border-slate-700">
                  <span v-if="key.active" class="inline-block rounded px-2 py-0.5 text-[11px] font-semibold bg-green-50 dark:bg-green-950/30 text-green-600 dark:text-green-400">Active</span>
                  <span v-else class="inline-block rounded px-2 py-0.5 text-[11px] font-semibold bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-600">Revoked</span>
                </td>
                <td class="px-3 py-2.5 border-b border-slate-200 dark:border-slate-700">
                  <template v-if="key.active">
                    <template v-if="confirmRevokeId === key.id">
                      <AppButton variant="danger" size="sm" class="mr-1" @click="revokeKey(key)">
                        Confirm
                      </AppButton>
                      <AppButton variant="secondary" size="sm" @click="confirmRevokeId = null">
                        Cancel
                      </AppButton>
                    </template>
                    <button v-else type="button" class="bg-transparent border-none text-slate-400 dark:text-slate-600 cursor-pointer text-sm px-2 py-1 rounded hover:bg-red-50 dark:hover:bg-red-950/30 hover:text-red-600 dark:hover:text-red-400" @click="confirmRevokeId = key.id">
                      Revoke
                    </button>
                  </template>
                </td>
              </tr>
            </tbody>
          </table>
          <div class="mt-4 border-t border-slate-200 dark:border-slate-700 pt-4">
            <h3 class="text-sm font-semibold mb-2 text-slate-700 dark:text-slate-300">
              Historical Data
            </h3>
            <button
              type="button"
              class="text-sm px-3 py-1.5 rounded bg-blue-600 text-white hover:bg-blue-700"
              @click="startImport"
            >
              Import Session History
            </button>
            <p v-if="importStatus" class="text-xs text-slate-500 mt-1">
              {{ importStatus }}
            </p>
          </div>
        </section>

        <!-- Remotes -->
        <section v-else-if="activeSection === 'remotes' && authEnabled">
          <RemoteSettings />
        </section>

        <!-- Permission presets -->
        <section v-else-if="activeSection === 'permissionPresets'">
          <h3 class="text-[17px] font-bold text-slate-900 dark:text-slate-100 mb-1">
            Permissions
          </h3>
          <p class="text-xs text-slate-400 dark:text-slate-600 mb-5">
            Auto-saved tool permissions per project. Reset removes all stored permissions for this project.
          </p>
          <div v-if="presetsLoading" class="text-center py-12 text-slate-400 dark:text-slate-600 text-sm">
            Loading...
          </div>
          <p v-else-if="presetsError" class="text-xs text-red-600 dark:text-red-400 mb-3">
            {{ presetsError }}
          </p>
          <div v-else-if="presets.length === 0" class="text-center py-8 text-slate-400 dark:text-slate-600 text-sm">
            No saved permissions.
          </div>
          <table v-else class="w-full border-collapse text-[13px]">
            <thead>
              <tr>
                <th class="text-left text-[10px] uppercase tracking-wide text-slate-400 dark:text-slate-600 px-3 py-2 border-b border-slate-200 dark:border-slate-700">
                  Project
                </th>
                <th class="text-left text-[10px] uppercase tracking-wide text-slate-400 dark:text-slate-600 px-3 py-2 border-b border-slate-200 dark:border-slate-700">
                  Count
                </th>
                <th class="text-left text-[10px] uppercase tracking-wide text-slate-400 dark:text-slate-600 px-3 py-2 border-b border-slate-200 dark:border-slate-700">
                  Actions
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="p in presets" :key="p.cwd">
                <td class="px-3 py-2.5 border-b border-slate-200 dark:border-slate-700 text-slate-900 dark:text-slate-100 font-mono text-xs break-all">
                  {{ p.cwd }}
                </td>
                <td class="px-3 py-2.5 border-b border-slate-200 dark:border-slate-700 text-slate-500 dark:text-slate-400 whitespace-nowrap">
                  {{ p.count }} {{ p.count === 1 ? 'Tool' : 'Tools' }}
                </td>
                <td class="px-3 py-2.5 border-b border-slate-200 dark:border-slate-700 whitespace-nowrap">
                  <template v-if="confirmResetCwd === p.cwd">
                    <AppButton variant="danger" size="sm" class="mr-1" @click="resetPresets(p.cwd)">
                      Yes, reset
                    </AppButton>
                    <AppButton variant="secondary" size="sm" @click="confirmResetCwd = null">
                      Cancel
                    </AppButton>
                  </template>
                  <button v-else type="button" class="bg-transparent border-none text-slate-400 dark:text-slate-600 cursor-pointer text-sm px-2 py-1 rounded hover:bg-red-50 dark:hover:bg-red-950/30 hover:text-red-600 dark:hover:text-red-400" @click="confirmResetCwd = p.cwd">
                    Reset
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </section>
      </div>
    </div>

    <!-- Create key dialog -->
    <Transition name="dialog">
      <div v-if="showCreateDialog" class="fixed inset-0 z-[200] bg-black/50 flex items-center justify-center" @click.self="closeCreateDialog">
        <div class="bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-xl w-full max-w-[480px] max-h-[90vh] overflow-y-auto shadow-[0_8px_32px_rgba(0,0,0,0.4)]">
          <header class="flex justify-between items-center px-5 py-4 border-b border-slate-200 dark:border-slate-700">
            <h2 class="text-lg font-semibold text-slate-900 dark:text-slate-100">
              Create API Key
            </h2>
            <button type="button" class="bg-transparent border-none text-slate-400 dark:text-slate-600 text-2xl cursor-pointer px-1 leading-none hover:text-slate-900 dark:hover:text-slate-100" @click="closeCreateDialog">
              &times;
            </button>
          </header>
          <form class="p-5" @submit.prevent="handleCreate">
            <div class="flex flex-col gap-1 mb-3.5">
              <label for="key-name" class="text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600">Name</label>
              <input
                id="key-name"
                v-model="newKeyName"
                class="w-full bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded px-2.5 py-1.5 text-sm text-slate-900 dark:text-slate-100 focus:outline-none focus:border-blue-500"
                type="text"
                required
                placeholder="e.g. CI pipeline key"
                autofocus
              >
            </div>
            <div class="flex flex-col gap-1 mb-3.5">
              <label for="key-group" class="text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600">Role / Scope Group</label>
              <select
                id="key-group"
                v-model="newKeyGroup"
                class="w-full bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded px-2.5 py-1.5 text-sm text-slate-900 dark:text-slate-100 focus:outline-none focus:border-blue-500"
              >
                <option value="viewer">
                  Viewer — tasks:read
                </option>
                <option value="operator">
                  Operator — tasks:read, pipeline:control
                </option>
                <option value="developer">
                  Developer — tasks:read, tasks:write, pipeline:control
                </option>
                <option value="admin">
                  Admin — all scopes
                </option>
              </select>
            </div>
            <p v-if="createError" class="text-xs text-red-600 dark:text-red-400 mb-2">
              {{ createError }}
            </p>
          </form>
          <footer class="flex justify-end gap-2 px-5 py-3 border-t border-slate-200 dark:border-slate-700">
            <AppButton variant="secondary" @click="closeCreateDialog">
              Cancel
            </AppButton>
            <AppButton variant="info" :disabled="isCreating || !newKeyName.trim()" @click="handleCreate">
              {{ isCreating ? 'Creating...' : 'Create Key' }}
            </AppButton>
          </footer>
        </div>
      </div>
    </Transition>

    <!-- Token reveal dialog -->
    <Transition name="dialog">
      <div v-if="revealedToken" class="fixed inset-0 z-[200] bg-black/50 flex items-center justify-center" @click.self="dismissReveal">
        <div class="bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-xl w-full max-w-[480px] max-h-[90vh] overflow-y-auto shadow-[0_8px_32px_rgba(0,0,0,0.4)]">
          <header class="flex justify-between items-center px-5 py-4 border-b border-slate-200 dark:border-slate-700">
            <h2 class="text-lg font-semibold text-slate-900 dark:text-slate-100">
              Your new API key
            </h2>
          </header>
          <div class="p-5">
            <p class="text-[13px] text-slate-600 dark:text-slate-400 mb-3">
              Save this token now — it will <strong class="text-yellow-600 dark:text-yellow-400">never be shown again</strong>.
            </p>
            <div class="font-mono text-xs bg-green-50 dark:bg-green-950/30 text-green-600 dark:text-green-400 p-3 rounded border border-green-200 dark:border-green-800/50 break-all mb-3">
              {{ revealedToken }}
            </div>
            <div class="flex justify-end">
              <AppButton variant="info" @click="copyToken">
                <span v-if="copyHint === '__error__'">Copy failed</span>
                <span v-else-if="copyHint">Copied!</span>
                <span v-else>Copy to clipboard</span>
              </AppButton>
            </div>
          </div>
          <footer class="flex justify-end gap-2 px-5 py-3 border-t border-slate-200 dark:border-slate-700">
            <AppButton variant="secondary" @click="dismissReveal">
              Done — I have saved the token
            </AppButton>
          </footer>
        </div>
      </div>
    </Transition>
  </AppModal>
</template>
