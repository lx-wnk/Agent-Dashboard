<script setup lang="ts">
import { ref } from 'vue'
import { usePluginSettings } from '../composables/usePluginSettings'
import { useServerReconnect } from '../composables/useServerReconnect'
import { errorMessage } from '../utils/errorMessage'
import PluginSettingsForm from './PluginSettingsForm.vue'
import PluginSlot from './PluginSlot.vue'

const { plugins, loading, error, setActive, getSettings, putSettings, update } = usePluginSettings()
const { triggerRestart } = useServerReconnect()
const saving = ref<string | null>(null)
const expanded = ref<string | null>(null)

function isBootWired(caps: string[]) {
  return caps.includes('auth_provider')
}

async function onRestart() {
  try {
    await triggerRestart()
  }
  catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}

const notice = ref<{ kind: 'success' | 'warning', text: string } | null>(null)
let noticeTimer: ReturnType<typeof setTimeout> | null = null

function showNotice(kind: 'success' | 'warning', text: string) {
  notice.value = { kind, text }
  if (noticeTimer)
    clearTimeout(noticeTimer)
  noticeTimer = setTimeout(() => (notice.value = null), 5000)
}

const reloadNotice = ref<string | null>(null)
function reloadPage() {
  window.location.reload()
}

const CAP_LABELS: Record<string, string> = {
  auth_provider: 'Auth Provider',
  route_extension: 'Route Extension',
  ui_extension: 'UI Extension',
}

function toggleExpand(id: string) {
  expanded.value = expanded.value === id ? null : id
}

async function handleToggle(id: string, next: boolean) {
  saving.value = id
  try {
    await setActive(id, next)
    const plugin = plugins.value.find(p => p.id === id)
    const isAuthProvider = plugin?.capabilities.includes('auth_provider') ?? false
    if (next && isAuthProvider) {
      showNotice('warning', `Plugin enabled — restart required; enabling will require login`)
    }
    else {
      showNotice('success', `Plugin ${next ? 'enabled' : 'disabled'}`)
    }
    if (!next && plugin?.capabilities.includes('ui_extension'))
      reloadNotice.value = 'Plugin UI disabled — reload the page to fully unload its code'
    else if (next)
      reloadNotice.value = null
  }
  catch (e) {
    error.value = errorMessage(e, 'Toggle failed')
  }
  finally {
    saving.value = null
  }
}

async function handleUpdate(id: string) {
  saving.value = id
  try {
    await update(id)
    showNotice('success', 'Plugin updated successfully')
  }
  catch (e) {
    error.value = errorMessage(e, 'Update failed')
  }
  finally {
    saving.value = null
  }
}
</script>

<template>
  <div class="space-y-4">
    <h3 class="text-sm font-semibold text-fg-soft">
      Loaded Plugins
    </h3>
    <p class="text-xs text-fg-mute">
      Sidecar plugins loaded from
      <code class="font-mono bg-raised px-1 rounded text-[11px]">plugin_dir</code>. Add plugins by placing a
      <code class="font-mono bg-raised px-1 rounded text-[11px]">plugin.json</code> + binary in the configured directory and restarting the server.
      Set <code class="font-mono bg-raised px-1 rounded text-[11px]">DASHBOARD_PLUGIN_DIR</code> to enable.
    </p>

    <div
      v-if="notice"
      role="status"
      aria-live="polite"
      class="text-xs rounded-md px-3 py-2"
      :class="notice.kind === 'warning' ? 'bg-warning-soft text-warning-text' : 'bg-success-soft text-success-text'"
    >
      {{ notice.text }}
    </div>

    <div v-if="reloadNotice" class="flex items-center gap-3 text-xs rounded-md px-3 py-2 bg-warning-soft text-warning-text" role="status">
      <span class="grow">{{ reloadNotice }}</span>
      <button type="button" data-action="reload-now" class="shrink-0 underline underline-offset-2" @click="reloadPage">
        Reload now
      </button>
      <button type="button" class="shrink-0" aria-label="Dismiss" @click="reloadNotice = null">
        ×
      </button>
    </div>

    <div v-if="loading" class="text-xs text-slate-400" role="status" aria-live="polite">
      Loading plugins…
    </div>
    <div v-else-if="error" class="text-xs text-danger-text" role="alert">
      {{ error }}
    </div>
    <div v-else-if="plugins.length === 0" class="text-xs text-slate-400 italic space-y-1">
      <p>No plugins loaded.</p>
      <p>
        Set <code class="font-mono bg-raised px-1 rounded">DASHBOARD_PLUGIN_DIR</code>
        to a directory containing plugin subdirectories, each with a
        <code class="font-mono bg-raised px-1 rounded">plugin.json</code> manifest.
      </p>
    </div>
    <div v-else class="space-y-2">
      <div
        v-for="p in plugins"
        :key="p.id"
        class="bg-raised/50 rounded p-3 text-xs"
        :class="{ 'opacity-60': p.state !== 'active' }"
      >
        <div class="flex items-start justify-between gap-4">
          <div class="space-y-1 min-w-0">
            <p class="font-mono font-medium text-fg flex items-center gap-1.5">
              <span
                class="inline-block h-2 w-2 rounded-full shrink-0"
                :class="p.state === 'active' && p.healthy
                  ? 'bg-success-text'
                  : p.state === 'active'
                    ? 'bg-warning-text'
                    : 'bg-line-strong'"
                :title="p.state === 'active' && !p.healthy
                  ? 'Active — not currently running'
                  : p.state === 'active'
                    ? 'Active'
                    : p.state"
                :aria-label="p.state === 'active' && !p.healthy
                  ? 'Active — not currently running'
                  : p.state === 'active'
                    ? 'Active'
                    : p.state"
              />
              {{ p.id }}
              <span v-if="isBootWired(p.capabilities)" class="ml-1 px-1.5 py-0.5 text-[10px] bg-warning-soft text-warning-text rounded">
                restart required
              </span>
              <button
                v-if="isBootWired(p.capabilities)"
                type="button"
                class="ml-1 text-[10px] text-accent underline hover:no-underline focus-visible:outline-none focus-visible:ring-[2px] focus-visible:ring-accent rounded"
                @click="onRestart"
              >
                Restart server
              </button>
            </p>
            <p class="text-fg-faint text-[10px]">
              v{{ p.version }}
            </p>
          </div>
          <div class="flex items-center gap-3 shrink-0">
            <div class="flex flex-wrap gap-1 justify-end">
              <span
                v-for="cap in p.capabilities"
                :key="cap"
                class="px-2 py-0.5 bg-blue-100 dark:bg-blue-900/40 text-blue-700 dark:text-blue-300 rounded-full"
              >
                {{ CAP_LABELS[cap] ?? cap }}
              </span>
            </div>
            <button
              v-if="p.updateAvailable"
              type="button"
              class="text-xs text-accent underline-offset-2 hover:underline disabled:opacity-50"
              :disabled="saving === p.id"
              @click="handleUpdate(p.id)"
            >
              Update
            </button>
            <button
              v-if="p.hasSettings"
              type="button"
              class="text-xs text-accent underline-offset-2 hover:underline"
              @click="toggleExpand(p.id)"
            >
              {{ expanded === p.id ? 'Hide settings' : 'Settings' }}
            </button>
            <button
              type="button"
              role="switch"
              :aria-checked="p.state === 'active'"
              :aria-label="`Enable plugin ${p.id}`"
              class="relative inline-flex h-6 w-11 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:ring-offset-2 dark:focus-visible:ring-offset-slate-900 disabled:cursor-not-allowed"
              :class="p.state === 'active' ? 'bg-accent' : 'bg-line-strong'"
              :disabled="saving === p.id"
              @click="handleToggle(p.id, p.state !== 'active')"
            >
              <span
                class="pointer-events-none inline-block h-4 w-4 rounded-full bg-white shadow transform transition-transform"
                :class="p.state === 'active' ? 'translate-x-5' : 'translate-x-0'"
              />
            </button>
          </div>
        </div>
        <PluginSettingsForm
          v-if="expanded === p.id"
          :plugin-id="p.id"
          :get-settings="getSettings"
          :put-settings="putSettings"
          class="mt-3 border-t border-line pt-3"
        />
      </div>
    </div>
    <PluginSlot name="settings-panel" :ctx="{}" />
  </div>
</template>
