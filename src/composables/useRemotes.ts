import { onMounted, ref } from 'vue'

export interface Remote {
  id: string
  url: string
  name: string | null
  createdAt: string
  connectionOk?: boolean
}

export function useRemotes() {
  const remotes = ref<Remote[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchRemotes() {
    loading.value = true
    error.value = null
    try {
      const res = await fetch('/api/remotes')
      if (res.ok)
        remotes.value = await res.json()
      else
        error.value = `Failed to load remotes (${res.status})`
    }
    catch {
      error.value = 'Network error loading remotes.'
    }
    finally {
      loading.value = false
    }
  }

  async function addRemote(url: string, name: string | null, bearerKey: string | null): Promise<Remote> {
    const res = await fetch('/api/remotes', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url, name, bearerKey }),
    })
    if (!res.ok) {
      const data = await res.json() as { error: string }
      throw new Error(data.error)
    }
    const reg = await res.json() as Remote
    remotes.value.push(reg)
    return reg
  }

  async function removeRemote(id: string): Promise<void> {
    await fetch(`/api/remotes/${id}`, { method: 'DELETE' })
    remotes.value = remotes.value.filter(r => r.id !== id)
  }

  onMounted(fetchRemotes)

  return {
    remotes,
    loading,
    error,
    refetch: fetchRemotes,
    addRemote,
    removeRemote,
  }
}
