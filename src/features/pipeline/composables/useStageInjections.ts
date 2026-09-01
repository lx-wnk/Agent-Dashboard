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

interface RunResult {
  id: string
  rows: MemoryInjection[]
  denied: string | null
  error: string | null
}

// Writes nothing shared: every outcome travels back to load(), so a batch that
// lost the race cannot leave a denial or an error behind on its way out.
async function fetchOne(stageRunId: string): Promise<RunResult> {
  const result: RunResult = { id: stageRunId, rows: [], denied: null, error: null }
  try {
    const res = await fetch(`/api/memory/injections?stageRun=${encodeURIComponent(stageRunId)}`)
    if (res.status === 403) {
      const body = await res.json().catch(() => ({})) as { error?: string }
      result.denied = body.error || DENIED_FALLBACK
      return result
    }
    if (!res.ok)
      throw new Error(`Failed to load memory injections (HTTP ${res.status})`)
    result.rows = await res.json() as MemoryInjection[]
  }
  catch (e) {
    result.error = errorMessage(e, 'Failed to load memory injections')
  }
  return result
}

export function useStageInjections(stageRuns: Ref<StageRun[]>) {
  const byStageRun = ref<Record<string, MemoryInjection[]>>({})
  const loading = ref(false)
  // One flag for the whole tab. The route gates at global scope whichever run
  // is asked for, so a denial is a property of the session, not of a row —
  // repeated per run it would read as N separate problems.
  const denied = ref<string | null>(null)
  // Held apart from `denied`: a refused read and a broken one have different
  // fixes, and the tab renders nothing for a run without an injection, so the
  // two must not both arrive as silence.
  const error = ref<string | null>(null)

  // Every refresh of the modal's stage runs starts a new batch of N requests,
  // and the older batch can answer last. Applying it would drop the newer
  // batch's runs and show "no memory push" for a run that has one.
  let latest = 0

  async function load(runs: StageRun[]): Promise<void> {
    const batch = ++latest
    denied.value = null
    error.value = null
    // No runs means no request was issued — not a read still in flight.
    loading.value = runs.length > 0

    const results = await Promise.all(runs.map(run => fetchOne(run.id)))
    if (batch !== latest)
      return

    const next: Record<string, MemoryInjection[]> = {}
    for (const result of results) {
      denied.value ??= result.denied
      error.value ??= result.error
      if (result.rows.length)
        next[result.id] = result.rows
    }
    byStageRun.value = next
    loading.value = false
  }

  // immediate, because the tab mounts before the modal's stage-run fetch
  // resolves; the array is replaced on every detail refresh, which is also
  // when a just-spawned run's injection first becomes readable.
  watch(stageRuns, runs => void load(runs), { immediate: true })

  return { byStageRun, loading, denied, error }
}
