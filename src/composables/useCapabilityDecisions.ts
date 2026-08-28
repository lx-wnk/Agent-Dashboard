import { ref } from 'vue'

export function useCapabilityDecisions() {
  const resolvingIds = ref<Record<string, boolean>>({})

  async function resolve(id: string, decision: 'allow' | 'deny'): Promise<void> {
    if (resolvingIds.value[id])
      return
    resolvingIds.value = { ...resolvingIds.value, [id]: true }
    try {
      const res = await fetch('/api/capabilities/decisions/respond', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'Origin': window.location.origin },
        body: JSON.stringify({ id, decision }),
      })
      // A 404 means the ask already expired or was answered elsewhere — a normal race, not a failure.
      if (res.ok || res.status === 404)
        return
      const body = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
      throw new Error(body.error || `HTTP ${res.status}`)
    }
    finally {
      const next = { ...resolvingIds.value }
      delete next[id]
      resolvingIds.value = next
    }
  }

  return { resolvingIds, resolve }
}
