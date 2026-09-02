import { onMounted, ref } from 'vue'
import { readErrorMessage } from '@/utils/errorMessage'

// Mirrors grantResponse and capabilityResponse in
// server/internal/api/grants/handler.go. Both used to be ent rows encoded
// straight from their storage tags, which is why these were snake_case; the
// tags also carried omitempty, so limitCount 0 (meaning unlimited) and an
// empty contextRef (meaning global scope) never arrived at all.
export interface Capability {
  id: string
  name: string
  class: string
  enforceableBy: string[]
  requiresPattern: boolean
  reversible: boolean
  description: string
}

export interface Grant {
  id: string
  capabilityName: string
  contextKind: string
  contextRef: string
  pattern: string
  mode: string
  limitCount: number
  limitWindowSeconds: number
  expiresAt: string | null
  grantedBy: string
  grantedAt: string
  revokedAt: string | null
  revokedBy: string
  reason: string
  // Always present now that the response is a DTO; the entity's omitempty hid
  // it on every single-node install, where it is the empty string.
  nodeId: string
}

// The grant a legacy migration wrote before per-user attribution existed
// (server/internal/db/client.go).
export const LEGACY_GRANTED_BY = 'migration:legacy'

// Mirrors server/internal/capability/validate.go's ContextKinds(), ranked
// most specific first. Not fetched from the server: these are a fixed Go enum,
// not a catalogue like capabilities.
export const GRANT_CONTEXT_KINDS = ['agent_session', 'task', 'routine', 'application', 'project', 'global'] as const
export type GrantContextKind = typeof GRANT_CONTEXT_KINDS[number]
export const GRANT_CONTEXT_GLOBAL: GrantContextKind = 'global'

// Mirrors validate.go's Modes(), ordered deny, allow, ask.
export const GRANT_MODES = ['deny', 'allow', 'ask'] as const
export type GrantMode = typeof GRANT_MODES[number]

// Body of POST /api/grants (server/internal/api/grants/handler.go createGrantRequest),
// which — unlike the ent rows above — is a hand-written DTO with camelCase tags.
export interface CreateGrantInput {
  capabilityName: string
  contextKind: GrantContextKind
  contextRef: string
  pattern: string
  mode: GrantMode
  limitCount: number
  limitWindowSeconds: number
  expiresInSeconds?: number
  reason: string
}

export function useGrants() {
  const grants = ref<Grant[]>([])
  const capabilities = ref<Capability[]>([])
  const loading = ref(true)
  const error = ref<string | null>(null)

  async function fetchCapabilities(): Promise<void> {
    const res = await fetch('/api/capabilities')
    if (!res.ok)
      throw new Error(await readErrorMessage(res, `HTTP ${res.status}`))
    capabilities.value = await res.json()
  }

  async function fetchGrants(capabilityFilter?: string): Promise<void> {
    loading.value = true
    error.value = null
    try {
      const url = capabilityFilter ? `/api/grants?capability=${encodeURIComponent(capabilityFilter)}` : '/api/grants'
      const res = await fetch(url)
      if (!res.ok)
        throw new Error(await readErrorMessage(res, `HTTP ${res.status}`))
      grants.value = await res.json()
    }
    catch (e) {
      error.value = e instanceof Error ? e.message : 'Failed to load grants'
    }
    finally {
      loading.value = false
    }
  }

  async function createGrant(input: CreateGrantInput): Promise<Grant> {
    const res = await fetch('/api/grants', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(input),
    })
    if (!res.ok)
      throw new Error(await readErrorMessage(res, 'Failed to create grant'))
    const grant = await res.json() as Grant
    grants.value.unshift(grant)
    return grant
  }

  // No response body on success (204) — refetch so revokedAt/revokedBy come
  // from the server rather than being guessed on the client.
  async function revokeGrant(id: string, capabilityFilter?: string): Promise<void> {
    const res = await fetch(`/api/grants/${id}`, { method: 'DELETE' })
    if (!res.ok)
      throw new Error(await readErrorMessage(res, 'Failed to revoke grant'))
    await fetchGrants(capabilityFilter)
  }

  onMounted(() => {
    void fetchCapabilities()
    void fetchGrants()
  })

  return {
    grants,
    capabilities,
    loading,
    error,
    fetchGrants,
    createGrant,
    revokeGrant,
  }
}
