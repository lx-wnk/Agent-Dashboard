import type { ProjectFolder } from '@/types'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  createFolder,
  deleteFolder,
  fetchProjectFolders,
  suggestFolders,
  updateFolder,
  useProjectFolders,
} from '../useProjectFolders'

function makeFolder(overrides: Partial<ProjectFolder> = {}): ProjectFolder {
  return {
    id: 'f1',
    projectId: 'p1',
    path: '/repo',
    isDefault: false,
    createdAt: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

let fetchMock: ReturnType<typeof vi.fn>

beforeEach(() => {
  fetchMock = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('fetchProjectFolders', () => {
  it('returns the parsed folder list on success', async () => {
    fetchMock.mockResolvedValue({ ok: true, json: () => Promise.resolve([makeFolder()]) })

    const folders = await fetchProjectFolders('p1')

    expect(fetchMock).toHaveBeenCalledWith('/api/projects/p1/folders')
    expect(folders).toEqual([makeFolder()])
  })

  it('throws on a non-ok response', async () => {
    fetchMock.mockResolvedValue({ ok: false, status: 404 })

    await expect(fetchProjectFolders('p1')).rejects.toThrow('HTTP 404')
  })
})

describe('suggestFolders', () => {
  it('returns the suggested folders on success', async () => {
    fetchMock.mockResolvedValue({ ok: true, json: () => Promise.resolve([makeFolder({ id: 'f2' })]) })

    const folders = await suggestFolders('p1')

    expect(fetchMock).toHaveBeenCalledWith('/api/projects/p1/folders/suggest')
    expect(folders).toEqual([makeFolder({ id: 'f2' })])
  })

  it('degrades to an empty array on a non-ok response instead of throwing', async () => {
    fetchMock.mockResolvedValue({ ok: false, status: 500 })

    await expect(suggestFolders('p1')).resolves.toEqual([])
  })
})

describe('createFolder', () => {
  it('createFolder posts the input and returns the created folder', async () => {
    const created = makeFolder({ id: 'f9', path: '/new' })
    fetchMock.mockResolvedValue({ ok: true, json: () => Promise.resolve(created) })

    const result = await createFolder('p1', { path: '/new' })

    expect(fetchMock).toHaveBeenCalledWith('/api/projects/p1/folders', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: '/new' }),
    })
    expect(result).toEqual(created)
  })

  it('throws the server error message on failure', async () => {
    fetchMock.mockResolvedValue({ ok: false, status: 400, json: () => Promise.resolve({ error: 'path required' }) })

    await expect(createFolder('p1', { path: '' })).rejects.toThrow('path required')
  })

  it('falls back to a generic message when the error body has no error field', async () => {
    fetchMock.mockResolvedValue({ ok: false, status: 500, json: () => Promise.resolve({}) })

    await expect(createFolder('p1', { path: '/x' })).rejects.toThrow('Failed to create folder')
  })

  it('surfaces HTTP status when the error body is not JSON', async () => {
    fetchMock.mockResolvedValue({ ok: false, status: 500, json: () => Promise.reject(new Error('bad json')) })

    await expect(createFolder('p1', { path: '/x' })).rejects.toThrow('HTTP 500')
  })
})

describe('updateFolder', () => {
  it('updateFolder patches the input and returns the updated folder', async () => {
    const updated = makeFolder({ label: 'Renamed' })
    fetchMock.mockResolvedValue({ ok: true, json: () => Promise.resolve(updated) })

    const result = await updateFolder('p1', 'f1', { label: 'Renamed' })

    expect(fetchMock).toHaveBeenCalledWith('/api/projects/p1/folders/f1', {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ label: 'Renamed' }),
    })
    expect(result.label).toBe('Renamed')
  })

  it('throws the server error message on failure', async () => {
    fetchMock.mockResolvedValue({ ok: false, status: 404, json: () => Promise.resolve({ error: 'folder not found' }) })

    await expect(updateFolder('p1', 'missing', { label: 'X' })).rejects.toThrow('folder not found')
  })
})

describe('deleteFolder', () => {
  it('issues a DELETE request', async () => {
    fetchMock.mockResolvedValue({ ok: true })

    await deleteFolder('p1', 'f1')

    expect(fetchMock).toHaveBeenCalledWith('/api/projects/p1/folders/f1', { method: 'DELETE' })
  })

  it('throws the server error message on failure', async () => {
    fetchMock.mockResolvedValue({ ok: false, status: 409, json: () => Promise.resolve({ error: 'folder in use' }) })

    await expect(deleteFolder('p1', 'f1')).rejects.toThrow('folder in use')
  })
})

describe('useProjectFolders', () => {
  it('load() populates folders on success', async () => {
    fetchMock.mockResolvedValue({ ok: true, json: () => Promise.resolve([makeFolder()]) })
    const { folders, isLoading, error, load } = useProjectFolders('p1')

    const pending = load()
    expect(isLoading.value).toBe(true)
    await pending

    expect(isLoading.value).toBe(false)
    expect(error.value).toBeNull()
    expect(folders.value).toEqual([makeFolder()])
  })

  it('load() sets an error and leaves folders empty on failure', async () => {
    fetchMock.mockResolvedValue({ ok: false, status: 500 })
    const { folders, error, load } = useProjectFolders('p1')

    await load()

    expect(error.value).toBe('HTTP 500')
    expect(folders.value).toEqual([])
  })

  it('suggest() delegates to suggestFolders for the bound projectId', async () => {
    fetchMock.mockResolvedValue({ ok: true, json: () => Promise.resolve([makeFolder({ id: 'sug' })]) })
    const { suggest } = useProjectFolders('p42')

    const result = await suggest()

    expect(fetchMock).toHaveBeenCalledWith('/api/projects/p42/folders/suggest')
    expect(result).toEqual([makeFolder({ id: 'sug' })])
  })
})
