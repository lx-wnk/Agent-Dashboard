<script setup lang="ts">
import { ref, onMounted } from 'vue'

interface PluginInfo {
  id: string
  capabilities: string[]
  base_url: string
}

const plugins = ref<PluginInfo[]>([])
const loading = ref(true)
const error = ref<string | null>(null)

onMounted(async () => {
  try {
    const res = await fetch('/api/plugins')
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    plugins.value = await res.json()
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Failed to load plugins'
  } finally {
    loading.value = false
  }
})

const CAP_LABELS: Record<string, string> = {
  auth_provider: 'Auth Provider',
  route_extension: 'Route Extension',
}
</script>

<template>
  <div class="space-y-4">
    <h3 class="text-sm font-semibold text-slate-700 dark:text-slate-300">Loaded Plugins</h3>
    <p class="text-xs text-slate-500 dark:text-slate-400">
      Sidecar plugins loaded from <code class="font-mono">plugin_dir</code>. Add plugins by placing a
      <code class="font-mono">plugin.json</code> + binary in the configured directory and restarting the server.
      Set <code class="font-mono">DASHBOARD_PLUGIN_DIR</code> to enable.
    </p>

    <div v-if="loading" class="text-xs text-slate-400">Loading plugins…</div>
    <div v-else-if="error" class="text-xs text-red-500">{{ error }}</div>
    <div v-else-if="plugins.length === 0" class="text-xs text-slate-400 italic">
      No plugins loaded. Configure <code class="font-mono">DASHBOARD_PLUGIN_DIR</code> to add plugins.
    </div>
    <div v-else class="space-y-2">
      <div
        v-for="p in plugins"
        :key="p.id"
        class="bg-slate-50 dark:bg-slate-800/50 rounded p-3 text-xs flex items-start justify-between gap-4"
      >
        <div class="space-y-1">
          <p class="font-mono font-medium text-slate-800 dark:text-slate-200">{{ p.id }}</p>
          <p class="text-slate-500">{{ p.base_url }}</p>
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
  </div>
</template>
