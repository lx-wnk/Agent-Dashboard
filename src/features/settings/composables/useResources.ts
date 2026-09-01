import { computed, onMounted, ref } from 'vue'
import { errorMessage } from '@/utils/errorMessage'

// Mirrors resourceView in server/internal/api/resources/handler.go — a
// hand-written camelCase DTO, not an ent row, so these names are the
// codebase's normal convention rather than useGrants' snake_case exception.
export interface ResourceView {
  id: string
  kind: string
  slug: string
  name: string
  scopeKind: string
  scopeRef: string
  nodeId: string
  state: string
  version: string
  origin: string
  originRef: string
  createdAt: string
  updatedAt: string
}

// Mirrors resourceKinds in server/internal/api/resources/handler.go, same
// order. The route answers 400 for anything else.
export const RESOURCE_KINDS = ['application', 'routine', 'skill', 'memory_space'] as const
export type ResourceKind = typeof RESOURCE_KINDS[number]

export const RESOURCE_KIND_LABELS: Record<ResourceKind, string> = {
  application: 'Applications',
  routine: 'Routines',
  skill: 'Skills',
  memory_space: 'Memory spaces',
}

// Mirrors memory.ScopeKinds (server/internal/memory/authorize.go:16), which is
// the accepted set for every transport's scope/scopeRef pair.
export const RESOURCE_SCOPE_KINDS = ['global', 'project', 'application'] as const
export type ResourceScopeKind = typeof RESOURCE_SCOPE_KINDS[number]

export interface ResourceQuery {
  kind: ResourceKind
  scopeKind: ResourceScopeKind
  scopeRef: string
}

export function useResources() {
  const resources = ref<ResourceView[]>([])
  const query = ref<ResourceQuery>({ kind: 'application', scopeKind: 'global', scopeRef: '' })
  const loading = ref(true)
  const error = ref<string | null>(null)

  // True while a non-global scope has no ref yet — the request below is
  // deliberately held rather than fired (see the guard in fetchResources).
  // Exposed here, not re-derived at the call site, so the "should we hold"
  // condition has exactly one owner.
  const held = computed(() => query.value.scopeKind !== 'global' && query.value.scopeRef.trim() === '')

  async function fetchResources(next?: Partial<ResourceQuery>): Promise<void> {
    if (next)
      query.value = { ...query.value, ...next }
    const { kind, scopeKind, scopeRef } = query.value

    // The server refuses a non-global scope with no ref (memory.ParseScope) —
    // hold the request rather than firing a known-400 on every keystroke.
    if (scopeKind !== 'global' && scopeRef.trim() === '') {
      resources.value = []
      loading.value = false
      error.value = null
      return
    }

    loading.value = true
    error.value = null
    try {
      const params = new URLSearchParams({ kind, scope: scopeKind })
      if (scopeKind !== 'global')
        params.set('scopeRef', scopeRef.trim())
      const res = await fetch(`/api/resources?${params.toString()}`)
      if (!res.ok)
        throw new Error(`Failed to load ${kind} resources (HTTP ${res.status})`)
      resources.value = await res.json()
    }
    catch (e) {
      // Clear on failure: keeping the previous kind's rows on screen under a
      // new kind's heading would misreport what the registry holds.
      resources.value = []
      error.value = errorMessage(e, 'Failed to load resources')
    }
    finally {
      loading.value = false
    }
  }

  onMounted(() => {
    void fetchResources()
  })

  return { resources, query, loading, error, held, fetchResources }
}
