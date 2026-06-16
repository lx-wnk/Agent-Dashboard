<script setup lang="ts">
import { usePlugins } from '../composables/usePlugins'
import PluginSlot from './PluginSlot.vue'

const { plugins, loading, error } = usePlugins()

const CAP_LABELS: Record<string, string> = {
  auth_provider: 'Auth Provider',
  route_extension: 'Route Extension',
  ui_extension: 'UI Extension',
}
</script>

<template>
  <div class="space-y-4">
    <h3 class="text-sm font-semibold text-fg-soft">Loaded Plugins</h3>
    <p class="text-xs text-fg-mute">
      Sidecar plugins loaded from
      <code class="font-mono bg-raised px-1 rounded text-[11px]">plugin_dir</code>. Add plugins by placing a
      <code class="font-mono bg-raised px-1 rounded text-[11px]">plugin.json</code> + binary in the configured directory and restarting the server.
      Set <code class="font-mono bg-raised px-1 rounded text-[11px]">DASHBOARD_PLUGIN_DIR</code> to enable.
    </p>

    <div v-if="loading" class="text-xs text-slate-400" role="status" aria-live="polite">Loading plugins…</div>
    <div v-else-if="error" class="text-xs text-red-500" role="alert">{{ error }}</div>
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
      >
        <div class="space-y-1">
          <p class="font-mono font-medium text-fg">{{ p.id }}</p>
        </div>
        <div class="flex flex-wrap gap-1">
          <span
            v-for="cap in p.capabilities"
            :key="cap"
            class="px-2 py-0.5 bg-blue-100 dark:bg-blue-900/40 text-blue-700 dark:text-blue-300 rounded-full"
          >
            {{ CAP_LABELS[cap] ?? cap }}
          </span>
        </div>
      </div>
    </div>
    <PluginSlot name="settings-panel" :ctx="{}" />
  </div>
</template>
