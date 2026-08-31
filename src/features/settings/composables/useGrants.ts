import { onMounted, ref } from 'vue'

// Grant and Capability are ent-generated rows (server/internal/db/ent/{grant,capability}.go)
// encoded straight from their Go struct tags, which are ent's default snake_case —
// unlike the rest of this codebase's camelCase DTOs, so the field names here
// intentionally do not match the sdk.generated.ts convention.
export interface Capability {
  id: string
  name: string
  class: string
  enforceable_by: string[]
  requires_pattern: boolean
  reversible: boolean
  description: string
}

export interface Grant {
  id: string
  capability_name: string
  context_kind: string
  context_ref: string
  pattern: string
  mode: string
  limit_count: number
  limit_window_seconds: number
  expires_at: string | null
  granted_by: string
  granted_at: string
  revoked_at: string | null
  revoked_by: string
  reason: string
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

async function readError(res: Response, fallback: string): Promise<string> {
  const body = await res.json().catch(() => ({ error: fallback })) as { error?: string }
  return body.error || fallback
}

export function useGrants() {
  const grants = ref<Grant[]>([])
  const capabilities = ref<Capability[]>([])
  const loading = ref(true)
  const error = ref<string | null>(null)

  async function fetchCapabilities(): Promise<void> {
    const res = await fetch('/api/capabilities')
    if (!res.ok)
      throw new Error(await readError(res, `HTTP ${res.status}`))
    capabilities.value = await res.json()
  }

  async function fetchGrants(capabilityFilter?: string): Promise<void> {
    loading.value = true
    error.value = null
    try {
      const url = capabilityFilter ? `/api/grants?capability=${encodeURIComponent(capabilityFilter)}` : '/api/grants'
      const res = await fetch(url)
      if (!res.ok)
        throw new Error(await readError(res, `HTTP ${res.status}`))
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
      throw new Error(await readError(res, 'Failed to create grant'))
    const grant = await res.json() as Grant
    grants.value.unshift(grant)
    return grant
  }

  // No response body on success (204) — refetch so revoked_at/revoked_by come
  // from the server rather than being guessed on the client.
  async function revokeGrant(id: string, capabilityFilter?: string): Promise<void> {
    const res = await fetch(`/api/grants/${id}`, { method: 'DELETE' })
    if (!res.ok)
      throw new Error(await readError(res, 'Failed to revoke grant'))
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
