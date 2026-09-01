import type { ResourceScopeKind, ResourceView } from '@/features/settings/composables/useResources'
import { computed, onMounted, ref } from 'vue'
import { errorMessage, readErrorMessage } from '@/utils/errorMessage'

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

// Mirrors the kind column's documented values in memory_entry.go.
export const MEMORY_ENTRY_KINDS = ['fact', 'preference', 'lesson', 'entity', 'pointer'] as const
export type MemoryEntryKind = typeof MEMORY_ENTRY_KINDS[number]

// Mirrors the source_kind column's documented values in memory_entry.go.
export const MEMORY_SOURCE_KINDS = ['agent', 'user', 'application', 'import'] as const
export type MemorySourceKind = typeof MEMORY_SOURCE_KINDS[number]

// A 403 from a write route is a configuration state with a known fix, not a
// failure of the request — memory.write is a separate grant from memory.read,
// so it must be distinguishable from a transport error to be explained as one
// (cf. `denied` on the read side).
export class MemoryWriteDeniedError extends Error {}

// Body of POST /api/memory/spaces (createSpaceBody in
// server/internal/api/memory/handler.go) minus scope/scopeRef: those are
// filled in below from the panel's current scope, so no caller can omit them.
// mem.ParseScope turns a missing scope into global with no error, which would
// authorize — and write — in the wrong scope with nothing reporting it.
export interface CreateSpaceInput {
  slug: string
  name: string
}

// Body of POST /api/memory/entries (createEntryBody, same file), minus
// scope/scopeRef for the same reason.
export interface CreateEntryInput {
  spaceSlug: string
  summary: string
  content: string
  kind: MemoryEntryKind
  sourceKind: MemorySourceKind
  sourceRef: string
  confidence: number
}

export interface MemoryScope {
  scopeKind: ResourceScopeKind
  scopeRef: string
}

const GLOBAL_SCOPE: MemoryScope = { scopeKind: 'global', scopeRef: '' }

// States only what is known. handler.go's authorize() maps every Gate.Authorize
// failure to 403 — a missing grant, a rate limit and a failed read of the grant
// store alike — so neither fallback names a cause. The likely one is named once,
// by the notice that renders these (cf. useStageInjections' DENIED_FALLBACK).
const READ_DENIED_FALLBACK = 'The memory route refused this read (HTTP 403) without giving a reason.'
const WRITE_DENIED_FALLBACK = 'The memory route refused this write (HTTP 403) without giving a reason.'

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
  // Own ref from `denied` for the same reason `searchError` is own from
  // `error`: the panel renders `denied` in place of the spaces table, so a
  // search refused in a scope whose spaces loaded perfectly would otherwise
  // blank a table the read grant legitimately filled.
  const searchDenied = ref<string | null>(null)
  // In flight, and answered-with-rows. Without both, "searching", "never
  // asked" and "found nothing" are one identical empty list.
  const searching = ref(false)
  const searched = ref(false)

  // True while a non-global scope has no ref yet — the request below is
  // deliberately held rather than fired (see the guard in fetchSpaces and
  // searchEntries). Exposed here, not re-derived at the call site, so
  // "should we hold" has exactly one owner (cf. useResources.held).
  const held = computed(() => scope.value.scopeKind !== 'global' && scope.value.scopeRef.trim() === '')

  // A request belongs to the scope it was made in, and the scope can change
  // while it is still in flight. Applying a superseded answer renders the
  // previous scope's rows under the new scope's heading — and, for the search,
  // asserts them as this scope's confirmed answer by also setting `searched`.
  // Same batch counter as useStageInjections: capture at the start, drop the
  // answer once a newer batch has started. One counter per ref written, so a
  // spaces refetch cannot discard an unrelated in-flight search.
  let latestSpaces = 0
  let latestGlobalSpaces = 0
  let latestSearch = 0

  async function fetchSpaces(): Promise<void> {
    const batch = ++latestSpaces
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
    let rows: MemorySpace[] = []
    let refusal: string | null = null
    let failure: string | null = null
    try {
      const res = await fetch(`/api/memory/spaces?${scopeParams(scope.value).toString()}`)
      if (res.status === 403)
        refusal = await readErrorMessage(res, READ_DENIED_FALLBACK)
      else if (!res.ok)
        throw new Error(await readErrorMessage(res, `HTTP ${res.status}`))
      else
        rows = await res.json()
    }
    catch (e) {
      failure = errorMessage(e, 'Failed to load memory spaces')
    }
    if (batch !== latestSpaces)
      return

    spaces.value = rows
    denied.value = refusal
    error.value = failure
    loading.value = false
  }

  // Fetches the global spaces list purely as a label-resolution source for
  // entries.value — never assigned to `spaces`. Skipped at global scope
  // (spaces already IS the global list there) and while held (nothing is
  // searchable yet). A failure here must not fail the panel: fall back to
  // the raw id, same as an unresolved space today.
  async function fetchGlobalSpacesForLabels(): Promise<void> {
    const batch = ++latestGlobalSpaces
    if (scope.value.scopeKind === 'global' || held.value) {
      globalSpaces.value = []
      return
    }
    let rows: MemorySpace[] = []
    try {
      const res = await fetch(`/api/memory/spaces?${scopeParams(GLOBAL_SCOPE).toString()}`)
      rows = res.ok ? await res.json() : []
    }
    catch {
      rows = []
    }
    // A late answer here marks hits as "outside this scope" (isOutsideScope)
    // against a list the current scope never asked for.
    if (batch !== latestGlobalSpaces)
      return
    globalSpaces.value = rows
  }

  async function searchEntries(): Promise<void> {
    const batch = ++latestSearch
    if (held.value) {
      entries.value = []
      searchError.value = null
      searchDenied.value = null
      searched.value = false
      return
    }
    searchError.value = null
    searchDenied.value = null
    searching.value = true
    let hits: MemoryEntryHit[] = []
    let refusal: string | null = null
    let failure: string | null = null
    // Only a request that answered with rows licenses "found nothing" — a
    // refused, failed or superseded one leaves the panel not knowing either way.
    let answered = false
    try {
      const params = scopeParams(scope.value)
      params.set('q', searchText.value)
      const res = await fetch(`/api/memory/entries?${params.toString()}`)
      if (res.status === 403) {
        refusal = await readErrorMessage(res, READ_DENIED_FALLBACK)
      }
      else if (!res.ok) {
        throw new Error(await readErrorMessage(res, `HTTP ${res.status}`))
      }
      else {
        hits = await res.json()
        answered = true
      }
    }
    catch (e) {
      failure = errorMessage(e, 'Failed to search memory')
    }
    if (batch !== latestSearch)
      return

    entries.value = hits
    searchDenied.value = refusal
    searchError.value = failure
    searched.value = answered
    searching.value = false
  }

  async function setScope(next: MemoryScope): Promise<void> {
    scope.value = next
    // Bumped even though no search is fired here: a search still in flight
    // belongs to the scope being left, and nothing else would supersede it.
    latestSearch++
    // Otherwise the previous scope's search hits survive the switch and
    // render under the new scope's heading — the same "more certainty than
    // was earned" defect this panel exists to avoid, inverted.
    entries.value = []
    searchError.value = null
    searchDenied.value = null
    searched.value = false
    // The dropped search will never reach its own reset, and "Searching
    // entries..." would otherwise stay on screen for good.
    searching.value = false
    await fetchSpaces()
    await fetchGlobalSpacesForLabels()
  }

  // The only place a write's scope comes from — the same `scope` ref
  // scopeParams reads for every read request.
  function scopeBody(): { scope: string, scopeRef: string } {
    return {
      scope: scope.value.scopeKind,
      scopeRef: scope.value.scopeKind === 'global' ? '' : scope.value.scopeRef.trim(),
    }
  }

  // A write carries the panel's scope, and the server refuses a non-global
  // one with a blank ref — held here for the same reason every read is,
  // reading the same computed rather than re-deriving the condition.
  function requireScopeRef(): void {
    if (held.value)
      throw new Error('Enter a scope ref before writing in this scope.')
  }

  // Throws rather than writing to a ref: a write is triggered by one control
  // and its outcome belongs next to that control, so the caller decides where
  // to render it. Never touches `error`/`denied`, which the spaces fetch owns.
  // No response body is read on success — DELETE answers 204 with none.
  async function send(url: string, init: RequestInit, fallback: string): Promise<void> {
    const res = await fetch(url, init)
    if (res.status === 403)
      throw new MemoryWriteDeniedError(await readErrorMessage(res, WRITE_DENIED_FALLBACK))
    if (!res.ok)
      throw new Error(await readErrorMessage(res, fallback))
  }

  function jsonInit(method: string, body: unknown): RequestInit {
    return { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }
  }

  // Refetched rather than splicing the created row in: which rows this scope
  // lists, and in what order, is ListSpaces' answer to give.
  async function createSpace(input: CreateSpaceInput): Promise<void> {
    requireScopeRef()
    await send('/api/memory/spaces', jsonInit('POST', { ...input, ...scopeBody() }), 'Failed to create space')
    await fetchSpaces()
  }

  // Same reason, more so: the entries list is a ranked search result, not a
  // table. Where a new entry lands — or whether it matches the current query
  // at all — is the server's bm25 ranking to decide.
  async function createEntry(input: CreateEntryInput): Promise<void> {
    requireScopeRef()
    await send('/api/memory/entries', jsonInit('POST', { ...input, ...scopeBody() }), 'Failed to create entry')
    await searchEntries()
  }

  // No scope on the body: the path carries only an entry id, and the server
  // resolves that entry's own space before authorizing.
  async function supersedeEntry(id: string, supersededBy: string): Promise<void> {
    await send(`/api/memory/entries/${encodeURIComponent(id)}`, jsonInit('PATCH', { supersededBy }), 'Failed to supersede entry')
    await searchEntries()
  }

  // Refetched so the entry's disappearance comes from the server's own
  // visibility rules (Retrieve drops expired hits) rather than being guessed.
  async function expireEntry(id: string): Promise<void> {
    await send(`/api/memory/entries/${encodeURIComponent(id)}`, { method: 'DELETE' }, 'Failed to expire entry')
    await searchEntries()
  }

  onMounted(() => {
    void fetchSpaces()
  })

  return { spaces, globalSpaces, entries, scope, searchText, loading, error, searchError, denied, searchDenied, searching, searched, held, fetchSpaces, searchEntries, setScope, createSpace, createEntry, supersedeEntry, expireEntry }
}
