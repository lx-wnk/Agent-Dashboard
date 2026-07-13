import { onMounted, ref } from 'vue'
import { errorMessage } from '@/utils/errorMessage'

export interface ProviderView {
  id: string
  displayName: string
  enabled: boolean
  configDirPresent: boolean
}

export function useProviderSettings() {
  const providers = ref<ProviderView[]>([])
  const loading = ref(true)
  const error = ref<string | null>(null)

  async function fetchProviders() {
    loading.value = true
    error.value = null
    try {
      const res = await fetch('/api/providers')
      if (!res.ok)
        throw new Error(`Failed to load providers (HTTP ${res.status})`)
      providers.value = await res.json()
    }
    catch (e) {
      error.value = errorMessage(e, 'Failed to load providers')
    }
    finally {
      loading.value = false
    }
  }

  async function toggle(id: string, enabled: boolean): Promise<void> {
    const res = await fetch(`/api/providers/${id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ enabled }),
    })
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    const saved: ProviderView = await res.json()
    providers.value = providers.value.map(p => (p.id === saved.id ? saved : p))
  }

  onMounted(fetchProviders)

  return { providers, loading, error, refetch: fetchProviders, toggle }
}
