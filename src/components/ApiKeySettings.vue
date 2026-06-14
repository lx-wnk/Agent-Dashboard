<script setup lang="ts">
import type { ApiKey, McpScope } from '../types'
import { computed, defineAsyncComponent, onMounted, onUnmounted, ref, watch } from 'vue'
import { useTheme } from '../composables/useTheme'
import { useUser } from '../composables/useUser'
import { useServerConfig } from '../composables/useServerConfig'
import { maskToken } from '../utils/format'
import { buildMcpAddCommand, buildMcpJsonConfig } from '../utils/mcpCommand'
const NotificationSettings = defineAsyncComponent(() => import('./NotificationSettings.vue'))
const PluginSettings = defineAsyncComponent(() => import('./PluginSettings.vue'))
import RemoteSettings from './RemoteSettings.vue'
import SystemPromptSettings from './SystemPromptSettings.vue'
import ProjectSettings from './ProjectSettings.vue'
import SpawnerSettings from './SpawnerSettings.vue'
import AuditLogTab from './AuditLogTab.vue'
import AppButton from './ui/AppButton.vue'
import AppModal from './ui/AppModal.vue'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: [] }>()

const { preference: themePref, setTheme } = useTheme()
const { authEnabled } = useUser()
const { mcpServerName, mcpEndpoint, loadServerConfig } = useServerConfig()

// --- Nav ---
type Section = 'appearance' | 'apiKeys' | 'remotes' | 'permissionPresets' | 'analytics' | 'systemPrompts' | 'plugins' | 'notifications' | 'projects' | 'spawners'
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
const revealedScopes = ref<McpScope[]>([])
const copiedTarget = ref<'token' | 'cli' | 'json' | null>(null)
const errorTarget = ref<'token' | 'cli' | 'json' | null>(null)
const tokenVisible = ref(false)

const mcpAddCommand = computed(() =>
  revealedToken.value && mcpServerName.value ? buildMcpAddCommand(window.location.origin, revealedToken.value, mcpServerName.value, mcpEndpoint.value) : '',
)
const mcpJsonConfig = computed(() =>
  revealedToken.value && mcpServerName.value ? buildMcpJsonConfig(window.location.origin, revealedToken.value, mcpServerName.value, mcpEndpoint.value) : '',
)
const mcpBlocks = computed(() => [
  { key: 'cli' as const, label: 'CLI command', labelId: 'mcp-cli-label', value: mcpAddCommand.value, extraClass: 'break-all' },
  { key: 'json' as const, label: 'JSON config', labelId: 'mcp-json-label', value: mcpJsonConfig.value, extraClass: 'whitespace-pre overflow-x-auto' },
])
const canAuthorTasks = computed(() => revealedScopes.value.includes('tasks:write'))

// Revoke / regenerate confirmation
const confirmRevokeId = ref<string | null>(null)
const confirmRegenerateId = ref<string | null>(null)
const isRegenerating = ref(false)

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

watch(activeSection, (val, oldVal) => {
  if (oldVal === 'apiKeys')
    dismissReveal()
  if (val === 'permissionPresets')
    void loadPresets()
  if (val === 'analytics')
    void loadPatterns()
})

// --- Analytics patterns ---
const patterns = ref<Array<{ tools: string, frequency: number }>>([])
const patternsLoading = ref(false)
const patternsError = ref<string | null>(null)

async function loadPatterns() {
  patternsError.value = null
  try {
    const res = await fetch('/api/analytics/patterns')
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    const data = await res.json() as { patterns: typeof patterns.value }
    patterns.value = data.patterns
  }
  catch (e) {
    patternsError.value = e instanceof Error ? e.message : 'Failed to load'
  }
}

async function refreshPatterns() {
  patternsLoading.value = true
  patternsError.value = null
  try {
    const res = await fetch('/api/analytics/patterns/refresh', { method: 'POST' })
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    await loadPatterns()
  }
  catch (e) {
    patternsError.value = e instanceof Error ? e.message : 'Failed to refresh'
  }
  finally {
    patternsLoading.value = false
  }
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    // AppModal handles ESC for showCreateDialog and revealedToken dialogs.
    // Only handle inline confirmation states here; fall through to outer close.
    if (confirmRevokeId.value || confirmRegenerateId.value || confirmResetCwd.value) {
      confirmRevokeId.value = null
      confirmRegenerateId.value = null
      confirmResetCwd.value = null
    }
    else {
      emit('close')
    }
  }
}
onMounted(() => {
  window.addEventListener('keydown', onKeydown)
  void loadServerConfig()
})
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

// --- Regenerate key ---
async function regenerateKey(key: ApiKey) {
  confirmRegenerateId.value = null
  isRegenerating.value = true
  try {
    const res = await fetch(`/api/settings/api-keys/${key.id}/regenerate`, { method: 'POST' })
    if (!res.ok) {
      const body = await res.json().catch(() => ({}))
      throw new Error(body.error ?? `HTTP ${res.status}`)
    }
    const data = await res.json() as { key: ApiKey, token: string }
    const idx = keys.value.findIndex(k => k.id === data.key.id)
    if (idx !== -1)
      keys.value.splice(idx, 1, data.key)
    else
      keys.value.unshift(data.key)
    revealedToken.value = data.token
    revealedScopes.value = data.key.scopes
  }
  catch (e) {
    errorMsg.value = (e as Error).message
  }
  finally {
    isRegenerating.value = false
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
    revealedScopes.value = data.key.scopes
    closeCreateDialog()
  }
  catch (e) {
    createError.value = (e as Error).message
  }
  finally {
    isCreating.value = false
  }
}

// --- Copy helpers ---
async function copyValue(target: 'token' | 'cli' | 'json', value: string) {
  if (!value)
    return
  try {
    await navigator.clipboard.writeText(value)
    copiedTarget.value = target
    errorTarget.value = null
  }
  catch {
    errorTarget.value = target
    copiedTarget.value = null
  }
  setTimeout(() => {
    copiedTarget.value = null
    errorTarget.value = null
  }, 2000)
}

function copyLabel(target: 'cli' | 'json', base: string) {
  if (copiedTarget.value === target)
    return 'Copied'
  if (errorTarget.value === target)
    return 'Copy failed'
  return base
}

function dismissReveal() {
  revealedToken.value = null
  revealedScopes.value = []
  copiedTarget.value = null
  errorTarget.value = null
  tokenVisible.value = false
}

// --- Helpers ---
function formatDate(iso: string | null) {
  if (!iso)
    return '—'
  return new Date(iso).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
}

// --- Historical data import ---
const importStatus = ref('')
const isImporting = ref(false)
let importEs: EventSource | null = null

onUnmounted(() => {
  importEs?.close()
})

async function startImport() {
  if (isImporting.value)
    return
  isImporting.value = true
  importStatus.value = 'Starting…'
  const res = await fetch('/api/history/import', { method: 'POST' })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    importStatus.value = `Error: ${(body as { error?: string }).error ?? res.statusText}`
    isImporting.value = false
    return
  }
  importEs = new EventSource('/api/history/import/status')
  importEs.onmessage = (ev) => {
    const p = JSON.parse(ev.data) as { total: number, processed: number, done: boolean }
    importStatus.value = `${p.processed}/${p.total} processed`
    if (p.done) {
      importStatus.value = `Import complete — ${p.processed} sessions processed`
      importEs?.close()
      importEs = null
      isImporting.value = false
    }
  }
  importEs.onerror = () => {
    importStatus.value = 'Connection lost — import may still be running'
    importEs?.close()
    importEs = null
    isImporting.value = false
  }
}
</script>

<template>
  <AppModal :open="open" size="auto" @close="emit('close')">
    <div class="bg-card rounded-xl border border-line shadow-[0_8px_40px_rgba(0,0,0,0.5)] w-[975px] max-w-[calc(100vw-2rem)] h-[700px] max-h-[85vh] flex overflow-hidden">
      <!-- ── Sidebar ──────────────────────────────── -->
      <nav class="w-[260px] flex-shrink-0 bg-app border-r border-line px-3 py-5 flex flex-col">
        <div class="flex items-center justify-between px-1 pb-1 mb-2">
          <h2 class="text-base font-bold text-fg">
            Settings
          </h2>
          <button type="button" class="bg-transparent border-none text-fg-mute text-2xl cursor-pointer px-1 leading-none hover:text-fg" @click="emit('close')">
            &times;
          </button>
        </div>
        <ul class="list-none p-0 m-0 flex flex-col gap-0.5">
          <li>
            <button
              type="button"
              class="w-full flex items-center gap-2 px-2.5 py-1.5 rounded-md border-none font-sans text-[13px] cursor-pointer text-left transition-colors"
              :class="activeSection === 'appearance'
                ? 'bg-raised text-fg font-semibold'
                : 'bg-transparent text-fg-mute hover:bg-raised/50 hover:text-fg'"
              :aria-current="activeSection === 'appearance' ? 'page' : undefined"
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
                ? 'bg-raised text-fg font-semibold'
                : 'bg-transparent text-fg-mute hover:bg-raised/50 hover:text-fg'"
              :aria-current="activeSection === 'apiKeys' ? 'page' : undefined"
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
                ? 'bg-raised text-fg font-semibold'
                : 'bg-transparent text-fg-mute hover:bg-raised/50 hover:text-fg'"
              :aria-current="activeSection === 'remotes' ? 'page' : undefined"
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
                ? 'bg-raised text-fg font-semibold'
                : 'bg-transparent text-fg-mute hover:bg-raised/50 hover:text-fg'"
              :aria-current="activeSection === 'permissionPresets' ? 'page' : undefined"
              @click="activeSection = 'permissionPresets'"
            >
              <span class="text-sm flex-shrink-0">⚿</span> Permissions
            </button>
          </li>
          <li>
            <button
              type="button"
              class="w-full flex items-center gap-2 px-2.5 py-1.5 rounded-md border-none font-sans text-[13px] cursor-pointer text-left transition-colors"
              :class="activeSection === 'analytics'
                ? 'bg-raised text-fg font-semibold'
                : 'bg-transparent text-fg-mute hover:bg-raised/50 hover:text-fg'"
              :aria-current="activeSection === 'analytics' ? 'page' : undefined"
              @click="activeSection = 'analytics'"
            >
              <span class="text-sm flex-shrink-0">📊</span> Analytics
            </button>
          </li>
          <li>
            <button
              type="button"
              class="w-full flex items-center gap-2 px-2.5 py-1.5 rounded-md border-none font-sans text-[13px] cursor-pointer text-left transition-colors"
              :class="activeSection === 'systemPrompts'
                ? 'bg-raised text-fg font-semibold'
                : 'bg-transparent text-fg-mute hover:bg-raised/50 hover:text-fg'"
              :aria-current="activeSection === 'systemPrompts' ? 'page' : undefined"
              @click="activeSection = 'systemPrompts'"
            >
              <span class="text-sm flex-shrink-0">✦</span> System Prompts
            </button>
          </li>
          <li>
            <button
              type="button"
              class="w-full flex items-center gap-2 px-2.5 py-1.5 rounded-md border-none font-sans text-[13px] cursor-pointer text-left transition-colors"
              :class="activeSection === 'plugins'
                ? 'bg-raised text-fg font-semibold'
                : 'bg-transparent text-fg-mute hover:bg-raised/50 hover:text-fg'"
              :aria-current="activeSection === 'plugins' ? 'page' : undefined"
              @click="activeSection = 'plugins'"
            >
              <span class="text-sm flex-shrink-0">🔌</span> Plugins
            </button>
          </li>
          <li>
            <button
              type="button"
              class="w-full flex items-center gap-2 px-2.5 py-1.5 rounded-md border-none font-sans text-[13px] cursor-pointer text-left transition-colors"
              :class="activeSection === 'notifications'
                ? 'bg-raised text-fg font-semibold'
                : 'bg-transparent text-fg-mute hover:bg-raised/50 hover:text-fg'"
              :aria-current="activeSection === 'notifications' ? 'page' : undefined"
              @click="activeSection = 'notifications'"
            >
              <span class="text-sm flex-shrink-0">🔔</span> Notifications
            </button>
          </li>
          <li>
            <button
              type="button"
              class="w-full flex items-center gap-2 px-2.5 py-1.5 rounded-md border-none font-sans text-[13px] cursor-pointer text-left transition-colors"
              :class="activeSection === 'projects'
                ? 'bg-raised text-fg font-semibold'
                : 'bg-transparent text-fg-mute hover:bg-raised/50 hover:text-fg'"
              :aria-current="activeSection === 'projects' ? 'page' : undefined"
              @click="activeSection = 'projects'"
            >
              <span class="text-sm flex-shrink-0">◫</span> Projects
            </button>
          </li>
          <li>
            <button
              type="button"
              class="w-full flex items-center gap-2 px-2.5 py-1.5 rounded-md border-none font-sans text-[13px] cursor-pointer text-left transition-colors"
              :class="activeSection === 'spawners'
                ? 'bg-raised text-fg font-semibold'
                : 'bg-transparent text-fg-mute hover:bg-raised/50 hover:text-fg'"
              :aria-current="activeSection === 'spawners' ? 'page' : undefined"
              @click="activeSection = 'spawners'"
            >
              <span class="text-sm flex-shrink-0">⚙</span> Spawners
            </button>
          </li>
        </ul>
        <div class="mt-auto pt-3 border-t border-line">
          <a
            class="text-xs text-fg-mute no-underline block px-1.5 py-1 rounded hover:text-slate-500 dark:hover:text-slate-400 transition-colors"
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
          <h3 class="text-[17px] font-bold text-fg mb-1">
            Themes
          </h3>
          <p class="text-xs text-fg-mute mb-5">
            Choose your preferred color scheme. Tip: press <kbd class="px-1 py-0.5 rounded bg-raised font-mono text-[11px]">Shift+D</kbd> anywhere to toggle dark/light mode.
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
                : 'border-line hover:border-blue-400'"
              :aria-label="opt.value === 'dark' ? `${opt.label} (keyboard shortcut: Shift+D)` : opt.label"
              :aria-pressed="themePref === opt.value"
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
              <div class="px-2.5 py-2 text-xs font-medium text-fg-mute border-t border-line flex items-center gap-1.5 bg-card">
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
              <h3 class="text-[17px] font-bold text-fg mb-1">
                API Keys
              </h3>
              <p class="text-xs text-fg-mute">
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
          <div v-if="isLoading" class="text-center py-12 text-fg-mute text-sm">
            Loading keys...
          </div>
          <div v-else-if="keys.length === 0 && !errorMsg" class="text-center py-8 text-fg-mute text-sm">
            No API keys yet. Create one to allow MCP clients to connect.
          </div>
          <table v-else class="w-full border-collapse text-[13px]">
            <thead>
              <tr>
                <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
                  Name
                </th>
                <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
                  Scopes
                </th>
                <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
                  Created
                </th>
                <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
                  Last Used
                </th>
                <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
                  Status
                </th>
                <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
                  Actions
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="key in keys" :key="key.id" :class="{ 'opacity-45': !key.active }">
                <td class="px-3 py-2.5 border-b border-line text-fg font-medium whitespace-nowrap">
                  {{ key.name }}
                </td>
                <td class="px-3 py-2.5 border-b border-line">
                  <div class="flex flex-wrap gap-1">
                    <span v-for="scope in key.scopes" :key="scope" class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute bg-raised px-1.5 py-px rounded font-mono">{{ scope }}</span>
                  </div>
                </td>
                <td class="px-3 py-2.5 border-b border-line text-fg-mute font-mono text-xs">
                  {{ formatDate(key.createdAt) }}
                </td>
                <td class="px-3 py-2.5 border-b border-line text-fg-mute font-mono text-xs">
                  {{ formatDate(key.lastUsedAt) }}
                </td>
                <td class="px-3 py-2.5 border-b border-line">
                  <span v-if="key.active" class="inline-block rounded px-2 py-0.5 text-[11px] font-semibold bg-green-50 dark:bg-green-950/30 text-green-600 dark:text-green-400">Active</span>
                  <span v-else class="inline-block rounded px-2 py-0.5 text-[11px] font-semibold bg-raised text-fg-mute">Revoked</span>
                </td>
                <td class="px-3 py-2.5 border-b border-line">
                  <template v-if="key.active">
                    <template v-if="confirmRevokeId === key.id">
                      <AppButton variant="danger" size="sm" class="mr-1" @click="revokeKey(key)">
                        Confirm
                      </AppButton>
                      <AppButton variant="secondary" size="sm" @click="confirmRevokeId = null">
                        Cancel
                      </AppButton>
                    </template>
                    <template v-else-if="confirmRegenerateId === key.id">
                      <AppButton variant="danger" size="sm" class="mr-1" :disabled="isRegenerating" @click="regenerateKey(key)">
                        Confirm
                      </AppButton>
                      <AppButton variant="secondary" size="sm" @click="confirmRegenerateId = null">
                        Cancel
                      </AppButton>
                    </template>
                    <template v-else>
                      <button type="button" class="bg-transparent border-none text-fg-mute cursor-pointer text-sm px-2 py-1 rounded hover:bg-amber-50 dark:hover:bg-amber-950/30 hover:text-amber-600 dark:hover:text-amber-400 mr-1" @click="confirmRegenerateId = key.id">
                        Regenerate
                      </button>
                      <button type="button" class="bg-transparent border-none text-fg-mute cursor-pointer text-sm px-2 py-1 rounded hover:bg-red-50 dark:hover:bg-red-950/30 hover:text-red-600 dark:hover:text-red-400" @click="confirmRevokeId = key.id">
                        Revoke
                      </button>
                    </template>
                  </template>
                </td>
              </tr>
            </tbody>
          </table>
          <div class="mt-4 border-t border-line pt-4">
            <h3 class="text-sm font-semibold mb-2 text-fg-soft">
              Historical Data
            </h3>
            <button
              type="button"
              :disabled="isImporting"
              class="text-sm px-3 py-1.5 rounded bg-blue-600 text-white hover:bg-blue-700 disabled:opacity-50"
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
          <h3 class="text-[17px] font-bold text-fg mb-1">
            Permissions
          </h3>
          <p class="text-xs text-fg-mute mb-5">
            Auto-saved tool permissions per project. Reset removes all stored permissions for this project.
          </p>
          <div v-if="presetsLoading" class="text-center py-12 text-fg-mute text-sm">
            Loading...
          </div>
          <p v-else-if="presetsError" class="text-xs text-red-600 dark:text-red-400 mb-3">
            {{ presetsError }}
          </p>
          <div v-else-if="presets.length === 0" class="text-center py-8 text-fg-mute text-sm">
            No saved permissions.
          </div>
          <table v-else class="w-full border-collapse text-[13px]">
            <thead>
              <tr>
                <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
                  Project
                </th>
                <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
                  Count
                </th>
                <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
                  Actions
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="p in presets" :key="p.cwd">
                <td class="px-3 py-2.5 border-b border-line text-fg font-mono text-xs break-all">
                  {{ p.cwd }}
                </td>
                <td class="px-3 py-2.5 border-b border-line text-fg-mute whitespace-nowrap">
                  {{ p.count }} {{ p.count === 1 ? 'Tool' : 'Tools' }}
                </td>
                <td class="px-3 py-2.5 border-b border-line whitespace-nowrap">
                  <template v-if="confirmResetCwd === p.cwd">
                    <AppButton variant="danger" size="sm" class="mr-1" @click="resetPresets(p.cwd)">
                      Yes, reset
                    </AppButton>
                    <AppButton variant="secondary" size="sm" @click="confirmResetCwd = null">
                      Cancel
                    </AppButton>
                  </template>
                  <button v-else type="button" class="bg-transparent border-none text-fg-mute cursor-pointer text-sm px-2 py-1 rounded hover:bg-red-50 dark:hover:bg-red-950/30 hover:text-red-600 dark:hover:text-red-400" @click="confirmResetCwd = p.cwd">
                    Reset
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </section>

        <!-- System Prompts -->
        <section v-else-if="activeSection === 'systemPrompts'">
          <SystemPromptSettings />
        </section>

        <!-- Plugins -->
        <section v-else-if="activeSection === 'plugins'">
          <PluginSettings />
        </section>

        <!-- Notifications -->
        <section v-else-if="activeSection === 'notifications'">
          <NotificationSettings />
        </section>

        <!-- Projects -->
        <section v-else-if="activeSection === 'projects'">
          <ProjectSettings />
        </section>

        <!-- Spawners -->
        <section v-else-if="activeSection === 'spawners'">
          <SpawnerSettings />
        </section>

        <!-- Analytics -->
        <section v-else-if="activeSection === 'analytics'">
          <h3 class="text-[17px] font-bold text-fg mb-1">
            Workflow Patterns
          </h3>
          <p class="text-xs text-fg-mute mb-5">
            Top 3-tool sequences discovered across all sessions.
          </p>
          <p v-if="patternsError" class="text-xs text-red-500 mb-3">
            {{ patternsError }}
          </p>
          <div v-else-if="patterns.length === 0" class="text-sm text-fg-mute">
            No patterns discovered yet.
          </div>
          <ul v-else class="space-y-1 mb-4">
            <li
              v-for="p in patterns"
              :key="p.tools"
              class="text-xs font-mono bg-raised px-2 py-1 rounded flex justify-between"
            >
              <span class="text-fg-soft">{{ p.tools }}</span>
              <span class="text-slate-400">×{{ p.frequency }}</span>
            </li>
          </ul>
          <button
            type="button"
            class="text-xs px-2 py-1 rounded border border-line-strong text-fg-mute hover:bg-slate-50 dark:hover:bg-slate-800 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            :disabled="patternsLoading"
            @click="refreshPatterns"
          >
            {{ patternsLoading ? 'Scanning…' : 'Refresh' }}
          </button>

          <div class="mt-8 pt-6 border-t border-line">
            <h3 class="text-[17px] font-bold text-fg mb-1">
              Audit Log
            </h3>
            <p class="text-xs text-fg-mute mb-4">
              Recent dashboard events — permission grants, task transitions, spawn actions.
            </p>
            <AuditLogTab :limit="100" hide-title />
          </div>
        </section>
      </div>
    </div>

    <!-- Create key dialog -->
    <AppModal :open="showCreateDialog" size="auto" :z-index="300" labelled-by="create-key-dialog-title" @close="closeCreateDialog">
      <div class="bg-card border border-line rounded-xl w-full max-w-[480px] max-h-[90vh] overflow-y-auto shadow-[0_8px_32px_rgba(0,0,0,0.4)]">
        <header class="flex justify-between items-center px-5 py-4 border-b border-line">
          <h2 id="create-key-dialog-title" class="text-lg font-semibold text-fg">
            Create API Key
          </h2>
          <button type="button" class="bg-transparent border-none text-fg-mute text-2xl cursor-pointer px-1 leading-none hover:text-fg" @click="closeCreateDialog">
            &times;
          </button>
        </header>
        <form class="p-5" @submit.prevent="handleCreate">
          <div class="flex flex-col gap-1 mb-3.5">
            <label for="key-name" class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">Name</label>
            <input
              id="key-name"
              v-model="newKeyName"
              class="w-full bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg focus:outline-none focus:border-blue-500"
              type="text"
              required
              placeholder="e.g. CI pipeline key"
              autofocus
            >
          </div>
          <div class="flex flex-col gap-1 mb-3.5">
            <label for="key-group" class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">Role / Scope Group</label>
            <select
              id="key-group"
              v-model="newKeyGroup"
              class="w-full bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg focus:outline-none focus:border-blue-500"
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
        <footer class="flex justify-end gap-2 px-5 py-3 border-t border-line">
          <AppButton variant="secondary" @click="closeCreateDialog">
            Cancel
          </AppButton>
          <AppButton variant="info" :disabled="isCreating || !newKeyName.trim()" @click="handleCreate">
            {{ isCreating ? 'Creating...' : 'Create Key' }}
          </AppButton>
        </footer>
      </div>
    </AppModal>

    <!-- Token reveal dialog -->
    <AppModal :open="!!revealedToken" size="auto" :z-index="300" labelled-by="token-reveal-dialog-title" @close="dismissReveal">
      <div class="bg-card border border-line rounded-xl w-full max-w-[480px] max-h-[90vh] overflow-y-auto shadow-[0_8px_32px_rgba(0,0,0,0.4)]">
        <header class="flex justify-between items-center px-5 py-4 border-b border-line">
          <h2 id="token-reveal-dialog-title" class="text-lg font-semibold text-fg">
            Your new API key
          </h2>
        </header>
        <div class="p-5">
          <p class="text-[13px] text-fg-mute mb-3">
            Save this token now — it will <strong class="text-amber-700 dark:text-yellow-400">never be shown again</strong>.
          </p>
          <div class="relative font-mono text-xs bg-green-50 dark:bg-green-950/30 text-green-600 dark:text-green-400 p-3 pr-10 rounded border border-green-200 dark:border-green-800/50 break-all mb-3">
            {{ tokenVisible ? revealedToken : maskToken(revealedToken ?? '') }}
            <button
              type="button"
              class="absolute right-2 top-1/2 -translate-y-1/2 p-1 rounded hover:bg-green-100 dark:hover:bg-green-900/40 text-green-500 transition-colors"
              :aria-label="tokenVisible ? 'Hide token' : 'Show token'"
              :aria-pressed="tokenVisible"
              @click="tokenVisible = !tokenVisible"
            >
              <svg v-if="tokenVisible" xmlns="http://www.w3.org/2000/svg" class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"/>
                <line x1="1" y1="1" x2="23" y2="23"/>
              </svg>
              <svg v-else xmlns="http://www.w3.org/2000/svg" class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/>
                <circle cx="12" cy="12" r="3"/>
              </svg>
            </button>
          </div>
          <div class="flex justify-end">
            <AppButton variant="info" @click="copyValue('token', revealedToken ?? '')">
              <span v-if="errorTarget === 'token'">Copy failed</span>
              <span v-else-if="copiedTarget === 'token'">Copied!</span>
              <span v-else>Copy to clipboard</span>
            </AppButton>
          </div>

          <div class="mt-5 border-t border-line pt-4">
            <p class="text-[13px] text-fg-mute mb-1">
              {{ canAuthorTasks ? "Connect a Claude Code session to this dashboard's task tools:" : "Connect a Claude Code session (this key has read-only access to task tools):" }}
            </p>
            <p v-if="!canAuthorTasks" class="text-[11px] text-amber-700 dark:text-yellow-400 mb-3">
              Read-only key — creating or refining tasks needs the Developer or Admin role.
            </p>

            <template v-for="b in mcpBlocks" :key="b.key">
              <span :id="b.labelId" class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">{{ b.label }}</span>
              <div
                role="region"
                :aria-labelledby="b.labelId"
                tabindex="0"
                class="relative font-mono text-xs bg-raised text-fg-soft p-3 pr-10 rounded border border-line mt-1 mb-3"
                :class="b.extraClass"
              >
                {{ b.value }}
                <button
                  type="button"
                  class="absolute right-1.5 top-1.5 p-1.5 rounded hover:bg-app text-fg-mute hover:text-fg transition-colors focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:ring-offset-1"
                  :aria-label="copyLabel(b.key, `Copy ${b.label}`)"
                  @click="copyValue(b.key, b.value)"
                >
                  <svg v-if="copiedTarget === b.key" xmlns="http://www.w3.org/2000/svg" class="w-4 h-4 text-green-500" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <polyline points="20 6 9 17 4 12"/>
                  </svg>
                  <svg v-else-if="errorTarget === b.key" xmlns="http://www.w3.org/2000/svg" class="w-4 h-4 text-red-500" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
                  </svg>
                  <svg v-else xmlns="http://www.w3.org/2000/svg" class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/>
                  </svg>
                </button>
              </div>
            </template>
            <p class="text-[11px] text-fg-mute mt-3">
              Contains your secret token — don't share it. The CLI command writes it to <code class="font-mono">~/.claude.json</code>; keep that file out of version control.
            </p>
          </div>
        </div>
        <footer class="flex justify-end gap-2 px-5 py-3 border-t border-line">
          <AppButton variant="secondary" @click="dismissReveal">
            Done — I have saved the token
          </AppButton>
        </footer>
      </div>
    </AppModal>
  </AppModal>
</template>
