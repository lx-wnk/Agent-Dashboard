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
  const entries = ref<MemoryEntryHit[]>([])
  const scope = ref<MemoryScope>({ scopeKind: 'global', scopeRef: '' })
  const searchText = ref('')
  const loading = ref(true)
  const error = ref<string | null>(null)
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

  async function searchEntries(): Promise<void> {
    if (held.value) {
      entries.value = []
      error.value = null
      denied.value = null
      return
    }
    error.value = null
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
      error.value = errorMessage(e, 'Failed to search memory')
      entries.value = []
    }
  }

  async function setScope(next: MemoryScope): Promise<void> {
    scope.value = next
    await fetchSpaces()
  }

  onMounted(() => {
    void fetchSpaces()
  })

  return { spaces, entries, scope, searchText, loading, error, denied, held, fetchSpaces, searchEntries, setScope }
}
