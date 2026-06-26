<script setup lang="ts">
import { ref } from 'vue'
import { usePluginSettings } from '../composables/usePluginSettings'
import { errorMessage } from '../utils/errorMessage'
import PluginSlot from './PluginSlot.vue'

const { plugins, loading, error, toggle } = usePluginSettings()
const saving = ref<string | null>(null)

const notice = ref<{ kind: 'success' | 'warning', text: string } | null>(null)
let noticeTimer: ReturnType<typeof setTimeout> | null = null

function showNotice(kind: 'success' | 'warning', text: string) {
  notice.value = { kind, text }
  if (noticeTimer)
    clearTimeout(noticeTimer)
  noticeTimer = setTimeout(() => (notice.value = null), 5000)
}

const CAP_LABELS: Record<string, string> = {
  auth_provider: 'Auth Provider',
  route_extension: 'Route Extension',
  ui_extension: 'UI Extension',
}

async function handleToggle(id: string, next: boolean) {
  saving.value = id
  try {
    const applied = await toggle(id, next)
    if (applied === 'restart')
      showNotice('warning', 'Takes effect after a server restart — enabling will require login')
    else
      showNotice('success', `Plugin ${next ? 'enabled' : 'disabled'}`)
  }
  catch (e) {
    error.value = errorMessage(e, 'Toggle failed')
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
        class="bg-raised/50 rounded p-3 text-xs flex items-start justify-between gap-4"
        :class="{ 'opacity-60': !p.enabled }"
      >
        <div class="space-y-1 min-w-0">
          <p class="font-mono font-medium text-fg flex items-center gap-1.5">
            <span
              class="inline-block h-2 w-2 rounded-full shrink-0"
              :class="p.healthy ? 'bg-success-text' : 'bg-line-strong'"
              :title="p.healthy ? 'Healthy' : 'Idle'"
              :aria-label="p.healthy ? 'Healthy' : 'Idle'"
            />
            {{ p.id }}
          </p>
          <p v-if="p.authProvider" class="text-fg-faint text-[10px]">
            Requires restart to apply
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
            type="button"
            role="switch"
            :aria-checked="p.enabled"
            :aria-label="`Enable plugin ${p.id}`"
            class="relative inline-flex h-6 w-11 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:ring-offset-2 dark:focus-visible:ring-offset-slate-900 disabled:cursor-not-allowed"
            :class="p.enabled ? 'bg-accent' : 'bg-line-strong'"
            :disabled="saving === p.id"
            @click="handleToggle(p.id, !p.enabled)"
          >
            <span
              class="pointer-events-none inline-block h-4 w-4 rounded-full bg-white shadow transform transition-transform"
              :class="p.enabled ? 'translate-x-5' : 'translate-x-0'"
            />
          </button>
        </div>
      </div>
    </div>
    <PluginSlot name="settings-panel" :ctx="{}" />
  </div>
</template>
