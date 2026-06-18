import type { ProjectFolder } from '../types'
import { ref } from 'vue'
import { errorMessage } from '../utils/errorMessage'

export interface CreateFolderInput {
  path: string
  label?: string
  isDefault?: boolean
}

export async function fetchProjectFolders(projectId: string): Promise<ProjectFolder[]> {
  const res = await fetch(`/api/projects/${projectId}/folders`)
  if (!res.ok)
    throw new Error(`HTTP ${res.status}`)
  return res.json() as Promise<ProjectFolder[]>
}

export async function suggestFolders(projectId: string): Promise<ProjectFolder[]> {
  const res = await fetch(`/api/projects/${projectId}/folders/suggest`)
  if (!res.ok)
    return []
  return res.json() as Promise<ProjectFolder[]>
}

export async function createFolder(projectId: string, input: CreateFolderInput): Promise<ProjectFolder> {
  const res = await fetch(`/api/projects/${projectId}/folders`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(input),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    throw new Error((err as { error: string }).error || 'Failed to create folder')
  }
  return res.json() as Promise<ProjectFolder>
}

export async function updateFolder(projectId: string, folderId: string, input: Partial<CreateFolderInput>): Promise<ProjectFolder> {
  const res = await fetch(`/api/projects/${projectId}/folders/${folderId}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(input),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    throw new Error((err as { error: string }).error || 'Failed to update folder')
  }
  return res.json() as Promise<ProjectFolder>
}

export async function deleteFolder(projectId: string, folderId: string): Promise<void> {
  const res = await fetch(`/api/projects/${projectId}/folders/${folderId}`, { method: 'DELETE' })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    throw new Error((err as { error: string }).error || 'Failed to delete folder')
  }
}

/**
 * Composable for managing a single project's folder list.
 * Lighter-weight than the project/spawner composables because folders are
 * always accessed in the context of a specific project — no global cache or
 * SSE stream needed.
 */
export function useProjectFolders(projectId: string) {
  const folders = ref<ProjectFolder[]>([])
  const isLoading = ref(false)
  const error = ref<string | null>(null)

  async function load(): Promise<void> {
    isLoading.value = true
    error.value = null
    try {
      folders.value = await fetchProjectFolders(projectId)
    }
    catch (e) {
      error.value = errorMessage(e)
    }
    finally {
      isLoading.value = false
    }
  }

  async function suggest(): Promise<ProjectFolder[]> {
    return suggestFolders(projectId)
  }

  return {
    folders,
    isLoading,
    error,
    load,
    suggest,
  }
}
