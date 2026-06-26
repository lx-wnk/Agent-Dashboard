import { onMounted, ref } from 'vue'
import { errorMessage } from '../utils/errorMessage'

export interface PluginInfo {
  id: string
  capabilities: string[]
  enabled: boolean
  healthy: boolean
  authProvider: boolean
}

export function usePluginSettings() {
  const plugins = ref<PluginInfo[]>([])
  const loading = ref(true)
  const error = ref<string | null>(null)

  async function fetchPlugins() {
    loading.value = true
    error.value = null
    try {
      const res = await fetch('/api/settings/plugins')
      if (!res.ok)
        throw new Error(`Failed to load plugins (HTTP ${res.status})`)
      plugins.value = await res.json()
    }
    catch (e) {
      error.value = errorMessage(e, 'Failed to load plugins')
    }
    finally {
      loading.value = false
    }
  }

  async function toggle(id: string, enabled: boolean): Promise<'live' | 'restart'> {
    const res = await fetch(`/api/settings/plugins-enabled/${id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ enabled }),
    })
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    const saved: { id: string, enabled: boolean, applied: 'live' | 'restart' } = await res.json()
    plugins.value = plugins.value.map(p => (p.id === saved.id ? { ...p, enabled: saved.enabled } : p))
    return saved.applied
  }

  onMounted(fetchPlugins)

  return { plugins, loading, error, refetch: fetchPlugins, toggle }
}
