import { onMounted, ref } from 'vue'

export interface PluginInfo {
  id: string
  capabilities: string[]
}

const plugins = ref<PluginInfo[]>([])
const loading = ref(true)
const error = ref<string | null>(null)

async function fetchPlugins() {
  loading.value = true
  error.value = null
  try {
    const res = await fetch('/api/settings/plugins', { credentials: 'same-origin' })
    if (!res.ok)
      throw new Error(`Failed to load plugins (HTTP ${res.status}: ${res.statusText})`)
    plugins.value = await res.json()
  }
  catch (e) {
    error.value = e instanceof Error ? e.message : 'Failed to load plugins'
  }
  finally {
    loading.value = false
  }
}

export function usePlugins() {
  onMounted(fetchPlugins)

  return {
    plugins,
    loading,
    error,
    refetch: fetchPlugins,
  }
}
