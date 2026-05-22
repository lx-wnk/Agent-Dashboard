import type { Project, ProjectFolder, Spawner } from '../types'
import { ref, shallowRef } from 'vue'

export interface UseSpawnDialogDeps {
  /** Returns the project's folder list. Caller decides whether to embed or fetch. */
  fetchFolders: (projectId: string) => Promise<ProjectFolder[]>
  /** Looks up a spawner by id from a local cache (e.g. useSpawners()). */
  lookupSpawner: (spawnerId: string) => Spawner | undefined
}

/**
 * State + actions for the project/folder/spawner hydration flow inside
 * the SpawnDialog modal. Decoupled from the SFC so it can be unit-tested
 * without rendering Vue.
 */
export function useSpawnDialog(deps: UseSpawnDialogDeps) {
  const project = shallowRef<Project | null>(null)
  const folders = shallowRef<ProjectFolder[]>([])
  const selectedFolderId = ref<string | null>(null)
  const cwd = ref('')
  const model = ref('')
  const spawnerId = ref<string | null>(null)

  async function selectProject(p: Project): Promise<void> {
    project.value = p
    const list = p.folders ?? await deps.fetchFolders(p.id)
    folders.value = list

    const defaultFolder = list.find(f => f.isDefault) ?? list[0] ?? null
    selectedFolderId.value = defaultFolder?.id ?? null
    cwd.value = defaultFolder?.path ?? ''

    if (p.defaultSpawnerId) {
      const sp = deps.lookupSpawner(p.defaultSpawnerId)
      spawnerId.value = sp?.id ?? null
      model.value = sp?.modelOverride ?? ''
    }
    else {
      spawnerId.value = null
      model.value = ''
    }
  }

  function selectFolder(folderId: string): void {
    const f = folders.value.find(x => x.id === folderId)
    if (!f)
      return
    selectedFolderId.value = folderId
    cwd.value = f.path
  }

  function clearProject(): void {
    project.value = null
    folders.value = []
    selectedFolderId.value = null
    cwd.value = ''
    model.value = ''
    spawnerId.value = null
  }

  return {
    project,
    folders,
    selectedFolderId,
    cwd,
    model,
    spawnerId,
    selectProject,
    selectFolder,
    clearProject,
  }
}
