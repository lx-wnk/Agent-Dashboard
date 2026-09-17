import { ref } from 'vue'

export interface ApplicationTool {
  capability: string
  name: string
  description?: string
  readOnlyHint: boolean
  destructiveHint?: boolean
}

export interface ApplicationSecret {
  envName: string
  updatedAt: string
}

export interface ApplicationView {
  resourceId: string
  serverName: string
  attachAll: boolean
  requiredEnv: string[]
  secrets: ApplicationSecret[]
  tools: ApplicationTool[]
  catalogueError?: string
  catalogueRefreshedAt?: string
}

async function readError(res: Response, fallback: string): Promise<string> {
  const body = await res.json().catch(() => null) as { error?: string } | null
  return body?.error || fallback
}

export function useApplications() {
  const applications = ref<ApplicationView[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  function replace(updated: ApplicationView) {
    applications.value = applications.value.map(a => a.resourceId === updated.resourceId ? updated : a)
  }

  async function fetchApplications(): Promise<void> {
    loading.value = true
    error.value = null
    try {
      const res = await fetch('/api/applications')
      if (!res.ok)
        throw new Error(await readError(res, `HTTP ${res.status}`))
      applications.value = await res.json() as ApplicationView[]
    }
    catch (e) {
      error.value = (e as Error).message || 'Failed to load applications'
    }
    finally {
      loading.value = false
    }
  }

  async function patch(resourceId: string, body: Record<string, unknown>): Promise<void> {
    const res = await fetch(`/api/applications/${encodeURIComponent(resourceId)}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
    if (!res.ok)
      throw new Error(await readError(res, 'Failed to update application'))
    replace(await res.json() as ApplicationView)
  }

  const setAttachAll = (resourceId: string, attachAll: boolean) => patch(resourceId, { attachAll })
  const setRequiredEnv = (resourceId: string, names: string[]) => patch(resourceId, { requiredEnv: names })

  async function setSecret(resourceId: string, envName: string, value: string): Promise<void> {
    const res = await fetch(`/api/applications/${encodeURIComponent(resourceId)}/secrets/${encodeURIComponent(envName)}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ value }),
    })
    if (!res.ok)
      throw new Error(await readError(res, 'Failed to store secret'))
    await fetchApplications()
  }

  async function deleteSecret(resourceId: string, envName: string): Promise<void> {
    const res = await fetch(`/api/applications/${encodeURIComponent(resourceId)}/secrets/${encodeURIComponent(envName)}`, { method: 'DELETE' })
    if (!res.ok)
      throw new Error(await readError(res, 'Failed to delete secret'))
    await fetchApplications()
  }

  async function refreshCatalogue(resourceId: string): Promise<void> {
    const res = await fetch(`/api/applications/${encodeURIComponent(resourceId)}/refresh`, { method: 'POST' })
    if (!res.ok)
      throw new Error(await readError(res, 'Failed to read the tool list'))
    replace(await res.json() as ApplicationView)
  }

  return { applications, loading, error, fetchApplications, setAttachAll, setRequiredEnv, setSecret, deleteSecret, refreshCatalogue }
}
