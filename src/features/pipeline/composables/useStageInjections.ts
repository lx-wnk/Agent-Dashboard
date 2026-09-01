import type { Ref } from 'vue'
import type { StageRun } from '@/types'
import { ref, watch } from 'vue'
import { errorMessage } from '@/utils/errorMessage'

// Mirrors memoryInjectionResponse (server/internal/api/memory/handler.go) as
// emitted by GET /api/memory/injections — one row per memory push into a stage
// spawn.
export interface MemoryInjection {
  id: string
  stageRunId: string
  entryIds: string[]
  charBudget: number
  charsUsed: number
  candidateCount: number
  createdAt: string
  updatedAt: string
}

// Used when the route refuses without a message of its own. It states only what
// is known — the read was refused — because handler.go maps every Gate.Authorize
// error to 403, a missing grant and a rate limit and a grant-store failure
// alike. The likely cause is named once, by the notice that renders this.
const DENIED_FALLBACK = 'The memory injections route refused this read (HTTP 403) without giving a reason.'

export function useStageInjections(stageRuns: Ref<StageRun[]>) {
  const byStageRun = ref<Record<string, MemoryInjection[]>>({})
  const loading = ref(false)
  // One flag for the whole tab. The route gates at global scope whichever runs
  // are asked for, so a denial is a property of the session, not of a row.
  const denied = ref<string | null>(null)
  // Held apart from `denied`: a refused read and a broken one have different
  // fixes, and the tab renders nothing for a run without an injection, so the
  // two must not both arrive as silence.
  const error = ref<string | null>(null)

  // Every refresh of the modal's stage runs starts a new request, and the older
  // one can answer last. Applying it would drop the newer batch's runs and show
  // "no memory push" for a run that has one.
  let latest = 0

  async function load(runs: StageRun[]): Promise<void> {
    const batch = ++latest
    denied.value = null
    error.value = null
    if (runs.length === 0) {
      byStageRun.value = {}
      loading.value = false
      return
    }
    loading.value = true

    // One request for the whole tab, not one per run: Gate.Authorize records a
    // rate-limit use on every call, and it is the same memory.read grant the
    // pipeline's own memory push spends. Per-run requests let opening a task
    // modal exhaust the window and silently stop agent memory retrieval — so
    // the fix is the route's repeated stageRun parameter, not client caching.
    const query = runs.map(run => `stageRun=${encodeURIComponent(run.id)}`).join('&')
    let rows: MemoryInjection[] = []
    let refusal: string | null = null
    let failure: string | null = null
    try {
      const res = await fetch(`/api/memory/injections?${query}`)
      if (res.status === 403) {
        const body = await res.json().catch(() => ({})) as { error?: string }
        refusal = body.error || DENIED_FALLBACK
      }
      else if (!res.ok) {
        throw new Error(`Failed to load memory injections (HTTP ${res.status})`)
      }
      else {
        rows = await res.json() as MemoryInjection[]
      }
    }
    catch (e) {
      failure = errorMessage(e, 'Failed to load memory injections')
    }
    if (batch !== latest)
      return

    const next: Record<string, MemoryInjection[]> = {}
    for (const row of rows)
      (next[row.stageRunId] ??= []).push(row)
    byStageRun.value = next
    denied.value = refusal
    error.value = failure
    loading.value = false
  }

  // immediate, because the tab mounts before the modal's stage-run fetch
  // resolves; the array is replaced on every detail refresh, which is also
  // when a just-spawned run's injection first becomes readable.
  watch(stageRuns, runs => void load(runs), { immediate: true })

  return { byStageRun, loading, denied, error }
}
