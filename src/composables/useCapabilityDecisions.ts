import { ref } from 'vue'

// Discriminated so a caller can tell "applied" apart from "the ask was
// already gone" apart from "the request actually failed" without parsing an
// error message or distinguishing a resolved-undefined from a throw.
export type ResolveOutcome
  = | { outcome: 'applied' }
    | { outcome: 'in-flight' }
    | { outcome: 'already-resolved' }
    | { outcome: 'error', message: string }

export function useCapabilityDecisions() {
  const resolvingIds = ref<Record<string, boolean>>({})

  async function resolve(id: string, decision: 'allow' | 'deny'): Promise<ResolveOutcome> {
    if (resolvingIds.value[id])
      return { outcome: 'in-flight' }
    resolvingIds.value = { ...resolvingIds.value, [id]: true }
    try {
      const res = await fetch('/api/capabilities/decisions/respond', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id, decision }),
      })
      if (res.ok)
        return { outcome: 'applied' }
      // A 404 means the ask already expired or was answered elsewhere — a normal race, not a failure.
      if (res.status === 404)
        return { outcome: 'already-resolved' }
      const body = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
      return { outcome: 'error', message: body.error || `HTTP ${res.status}` }
    }
    catch (e) {
      return { outcome: 'error', message: e instanceof Error ? e.message : String(e) }
    }
    finally {
      const next = { ...resolvingIds.value }
      delete next[id]
      resolvingIds.value = next
    }
  }

  return { resolvingIds, resolve }
}
