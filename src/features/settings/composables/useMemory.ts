import type { ResourceScopeKind, ResourceView } from '@/features/settings/composables/useResources'
import { computed, onMounted, ref } from 'vue'
import { errorMessage } from '@/utils/errorMessage'

// A memory space IS a registry row (kind = memory_space) — MemoryRepo.ListSpaces
// queries the same `resource` table ResourceRepo does. Aliased rather than
// redeclared so the two views cannot drift apart.
export type MemorySpace = ResourceView

// Mirrors memorySearchHitResponse (server/internal/api/memory/handler.go) —
// the *search hit* shape GET /api/memory/entries answers, narrower than the
// row POST/PATCH return.
export interface MemoryEntryHit {
  id: string
  spaceId: string
  summary: string
  content: string
  kind: string
  confidence: number
  createdAt: string
}

// Mirrors memoryEntryResponse (server/internal/api/memory/handler.go) as
// emitted by POST and PATCH /api/memory/entries.
export interface MemoryEntryRow {
  id: string
  spaceId: string
  summary: string
  content: string
  kind: string
  sourceKind: string
  sourceRef: string | null
  confidence: number
  validFrom: string
  validUntil: string | null
  supersededBy: string | null
  userId: string | null
  createdAt: string
  updatedAt: string
}

// Mirrors the kind column's documented values in memory_entry.go.
export const MEMORY_ENTRY_KINDS = ['fact', 'preference', 'lesson', 'entity', 'pointer'] as const
export type MemoryEntryKind = typeof MEMORY_ENTRY_KINDS[number]

// Mirrors the source_kind column's documented values in memory_entry.go.
export const MEMORY_SOURCE_KINDS = ['agent', 'user', 'application', 'import'] as const
export type MemorySourceKind = typeof MEMORY_SOURCE_KINDS[number]

export interface MemoryScope {
  scopeKind: ResourceScopeKind
  scopeRef: string
}

const GLOBAL_SCOPE: MemoryScope = { scopeKind: 'global', scopeRef: '' }

async function readError(res: Response, fallback: string): Promise<string> {
  const body = await res.json().catch(() => ({ error: fallback })) as { error?: string }
  return body.error || fallback
}

function scopeParams(scope: MemoryScope): URLSearchParams {
  const params = new URLSearchParams({ scope: scope.scopeKind })
  if (scope.scopeKind !== 'global')
    params.set('scopeRef', scope.scopeRef.trim())
  return params
}

export function useMemory() {
  const spaces = ref<MemorySpace[]>([])
  // Global spaces visible from the current non-global scope, fetched only to
  // resolve a hit's space label — Retriever.Retrieve unions every global
  // space into a scoped search (server/internal/memory/retrieve.go:165-192),
  // but ListSpaces filters on the exact scope, so `spaces` alone cannot name
  // a hit that came from a global space. Never rendered as a row in the
  // spaces table — that table's contract is "exactly this scope".
  const globalSpaces = ref<MemorySpace[]>([])
  const entries = ref<MemoryEntryHit[]>([])
  const scope = ref<MemoryScope>({ scopeKind: 'global', scopeRef: '' })
  const searchText = ref('')
  const loading = ref(true)
  const error = ref<string | null>(null)
  // Own ref from `error`: a failed search must not blank the already-loaded
  // spaces table (and, with it, the only retry control) the way a shared ref
  // rendered as a whole-panel replacement would.
  const searchError = ref<string | null>(null)
  // Held apart from `error`: a 403 means the capability gate refused, which is
  // a configuration state with a known fix, not a failure of the request.
  const denied = ref<string | null>(null)

  // True while a non-global scope has no ref yet — the request below is
  // deliberately held rather than fired (see the guard in fetchSpaces and
  // searchEntries). Exposed here, not re-derived at the call site, so
  // "should we hold" has exactly one owner (cf. useResources.held).
  const held = computed(() => scope.value.scopeKind !== 'global' && scope.value.scopeRef.trim() === '')

  async function fetchSpaces(): Promise<void> {
    // The server refuses a non-global scope with no ref (memory.ParseScope) —
    // hold the request instead of firing a known-400.
    if (held.value) {
      spaces.value = []
      loading.value = false
      error.value = null
      denied.value = null
      return
    }
    loading.value = true
    error.value = null
    denied.value = null
    try {
      const res = await fetch(`/api/memory/spaces?${scopeParams(scope.value).toString()}`)
      if (res.status === 403) {
        denied.value = await readError(res, 'memory.read is not granted in this scope')
        spaces.value = []
        return
      }
      if (!res.ok)
        throw new Error(await readError(res, `HTTP ${res.status}`))
      spaces.value = await res.json()
    }
    catch (e) {
      error.value = errorMessage(e, 'Failed to load memory spaces')
      spaces.value = []
    }
    finally {
      loading.value = false
    }
  }

  // Fetches the global spaces list purely as a label-resolution source for
  // entries.value — never assigned to `spaces`. Skipped at global scope
  // (spaces already IS the global list there) and while held (nothing is
  // searchable yet). A failure here must not fail the panel: fall back to
  // the raw id, same as an unresolved space today.
  async function fetchGlobalSpacesForLabels(): Promise<void> {
    if (scope.value.scopeKind === 'global' || held.value) {
      globalSpaces.value = []
      return
    }
    try {
      const res = await fetch(`/api/memory/spaces?${scopeParams(GLOBAL_SCOPE).toString()}`)
      globalSpaces.value = res.ok ? await res.json() : []
    }
    catch {
      globalSpaces.value = []
    }
  }

  async function searchEntries(): Promise<void> {
    if (held.value) {
      entries.value = []
      searchError.value = null
      denied.value = null
      return
    }
    searchError.value = null
    denied.value = null
    try {
      const params = scopeParams(scope.value)
      params.set('q', searchText.value)
      const res = await fetch(`/api/memory/entries?${params.toString()}`)
      if (res.status === 403) {
        denied.value = await readError(res, 'memory.read is not granted in this scope')
        entries.value = []
        return
      }
      if (!res.ok)
        throw new Error(await readError(res, `HTTP ${res.status}`))
      entries.value = await res.json()
    }
    catch (e) {
      searchError.value = errorMessage(e, 'Failed to search memory')
      entries.value = []
    }
  }

  async function setScope(next: MemoryScope): Promise<void> {
    scope.value = next
    // Otherwise the previous scope's search hits survive the switch and
    // render under the new scope's heading — the same "more certainty than
    // was earned" defect this panel exists to avoid, inverted.
    entries.value = []
    searchError.value = null
    await fetchSpaces()
    await fetchGlobalSpacesForLabels()
  }

  onMounted(() => {
    void fetchSpaces()
  })

  return { spaces, globalSpaces, entries, scope, searchText, loading, error, searchError, denied, held, fetchSpaces, searchEntries, setScope }
}
