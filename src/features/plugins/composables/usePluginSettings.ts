import { onMounted, ref } from 'vue'
import { errorMessage } from '@/utils/errorMessage'

export interface SettingField {
  key: string
  type: 'string' | 'url' | 'int' | 'bool' | 'enum'
  label: string
  secret: boolean
  enum?: string[]
}

export interface PluginView {
  id: string
  name: string
  version: string
  state: 'discovered' | 'inactive' | 'active'
  updateAvailable: boolean
  healthy: boolean
  capabilities: string[]
  hasSettings: boolean
}

export function usePluginSettings() {
  const plugins = ref<PluginView[]>([])
  const loading = ref(true)
  const error = ref<string | null>(null)

  async function fetchPlugins() {
    loading.value = true
    error.value = null
    try {
      const res = await fetch('/api/plugins', { credentials: 'same-origin' })
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

  // Live for route/ui extensions; auth_provider needs a restart (SP3b badge).
  async function setActive(id: string, active: boolean): Promise<void> {
    const verb = active ? 'activate' : 'deactivate'
    const res = await fetch(`/api/plugins/${id}/${verb}`, {
      method: 'POST',
      headers: { Origin: window.location.origin },
    })
    if (!res.ok) {
      let detail = `HTTP ${res.status}`
      try {
        const b = await res.json()
        if (b?.error)
          detail = b.error
      }
      catch { /* no body */ }
      throw new Error(detail)
    }
    plugins.value = plugins.value.map(p => (p.id === id ? { ...p, state: active ? 'active' : 'inactive' } : p))
  }

  async function getSettings(id: string): Promise<{ schema: SettingField[], values: Record<string, string> }> {
    const res = await fetch(`/api/plugins/${id}/settings`, { credentials: 'same-origin' })
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    return res.json()
  }

  async function putSettings(id: string, values: Record<string, string>): Promise<void> {
    const res = await fetch(`/api/plugins/${id}/settings`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json', 'Origin': window.location.origin },
      body: JSON.stringify({ values }),
    })
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
  }

  async function update(id: string): Promise<void> {
    const res = await fetch(`/api/plugins/${id}/update`, {
      method: 'POST',
      headers: { Origin: window.location.origin },
    })
    if (!res.ok) {
      let detail = `HTTP ${res.status}`
      try {
        const b = await res.json()
        if (b?.error)
          detail = b.error
      }
      catch { /* no body */ }
      throw new Error(detail)
    }
    await fetchPlugins()
  }

  onMounted(fetchPlugins)
  return { plugins, loading, error, refetch: fetchPlugins, setActive, getSettings, putSettings, update }
}
