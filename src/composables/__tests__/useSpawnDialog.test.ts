import type { Project, ProjectFolder, Spawner } from '../../types'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useSpawnDialog } from '../useSpawnDialog'

const sampleProject: Project = {
  id: 'prj_a',
  slug: 'alpha',
  name: 'Alpha',
  defaultSpawnerId: 'spwn_a',
  createdAt: '',
  updatedAt: '',
}

const sampleSpawner: Spawner = {
  id: 'spwn_a',
  name: 'Claude (Opus)',
  slug: 'claude-opus',
  command: 'claude',
  args: [],
  env: {},
  adapterType: 'claude',
  adapterConfig: {},
  modelOverride: 'claude-opus-4-7',
  builtIn: false,
  createdAt: '',
  updatedAt: '',
}

const singleFolder: ProjectFolder[] = [
  { id: 'fld_a', projectId: 'prj_a', path: '/home/u/alpha', isDefault: true, createdAt: '' },
]

const multiFolders: ProjectFolder[] = [
  { id: 'fld_a', projectId: 'prj_a', path: '/home/u/alpha-default', isDefault: true, createdAt: '' },
  { id: 'fld_b', projectId: 'prj_a', path: '/home/u/alpha-experimental', isDefault: false, createdAt: '' },
]

describe('useSpawnDialog', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('selecting a project with one folder hydrates cwd and model', async () => {
    const fetchFolders = vi.fn().mockResolvedValue(singleFolder)
    const lookupSpawner = vi.fn().mockReturnValue(sampleSpawner)

    const d = useSpawnDialog({ fetchFolders, lookupSpawner })
    await d.selectProject(sampleProject)

    expect(d.cwd.value).toBe('/home/u/alpha')
    expect(d.model.value).toBe('claude-opus-4-7')
    expect(d.spawnerId.value).toBe('spwn_a')
    expect(d.folders.value).toEqual(singleFolder)
    expect(d.selectedFolderId.value).toBe('fld_a')
  })

  it('selecting a project with multiple folders defaults to isDefault folder', async () => {
    const fetchFolders = vi.fn().mockResolvedValue(multiFolders)
    const lookupSpawner = vi.fn().mockReturnValue(sampleSpawner)

    const d = useSpawnDialog({ fetchFolders, lookupSpawner })
    await d.selectProject(sampleProject)

    expect(d.cwd.value).toBe('/home/u/alpha-default')
    expect(d.selectedFolderId.value).toBe('fld_a')
  })

  it('changing folder updates cwd', async () => {
    const fetchFolders = vi.fn().mockResolvedValue(multiFolders)
    const lookupSpawner = vi.fn().mockReturnValue(sampleSpawner)

    const d = useSpawnDialog({ fetchFolders, lookupSpawner })
    await d.selectProject(sampleProject)
    d.selectFolder('fld_b')

    expect(d.cwd.value).toBe('/home/u/alpha-experimental')
  })

  it('clearing project resets cwd, model, spawnerId', async () => {
    const fetchFolders = vi.fn().mockResolvedValue(singleFolder)
    const lookupSpawner = vi.fn().mockReturnValue(sampleSpawner)

    const d = useSpawnDialog({ fetchFolders, lookupSpawner })
    await d.selectProject(sampleProject)
    d.clearProject()

    expect(d.cwd.value).toBe('')
    expect(d.model.value).toBe('')
    expect(d.spawnerId.value).toBeNull()
    expect(d.folders.value).toEqual([])
  })

  it('project without defaultSpawnerId leaves model and spawnerId empty', async () => {
    const fetchFolders = vi.fn().mockResolvedValue(singleFolder)
    const lookupSpawner = vi.fn().mockReturnValue(undefined)

    const d = useSpawnDialog({ fetchFolders, lookupSpawner })
    await d.selectProject({ ...sampleProject, defaultSpawnerId: null })

    expect(d.model.value).toBe('')
    expect(d.spawnerId.value).toBeNull()
  })

  it('project with spawner but no modelOverride keeps model empty (Auto)', async () => {
    const fetchFolders = vi.fn().mockResolvedValue(singleFolder)
    const noOverride: Spawner = { ...sampleSpawner, modelOverride: undefined }
    const lookupSpawner = vi.fn().mockReturnValue(noOverride)

    const d = useSpawnDialog({ fetchFolders, lookupSpawner })
    await d.selectProject(sampleProject)

    expect(d.model.value).toBe('')
    expect(d.spawnerId.value).toBe('spwn_a')
  })
})
