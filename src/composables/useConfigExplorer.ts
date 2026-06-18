import { ref, shallowRef } from 'vue'

export interface SkillEntry {
  name: string
  source: string
  description: string
  path?: string
  editable: boolean
}

export interface CommandEntry {
  name: string
  source: string
  description: string
  body: string
  path?: string
  editable: boolean
}

export interface MemoryEntry {
  path: string
  scope: 'user' | 'project'
  size: number
  mtime: number
  editable: boolean
}

export interface ConfigFile {
  path: string
  content: string
  mtime: number
  editable: boolean
  source: string
}

export interface SaveResult {
  path: string
  mtime: number
  size: number
}

// ConflictError signals the server rejected a save because the file changed on
// disk since it was loaded (HTTP 409). Callers surface a reload prompt.
export class ConflictError extends Error {
  constructor(message = 'File was modified since it was loaded') {
    super(message)
    this.name = 'ConflictError'
  }
}

interface SkillsResponse { skills: SkillEntry[], scopeSource?: string, scopeLabel?: string }
interface CommandsResponse { commands: CommandEntry[], engineVersion?: string, builtinsMayBeStale?: boolean, scopeSource?: string, scopeLabel?: string }
interface MemoryResponse { memory: MemoryEntry[], scopeSource?: string, scopeLabel?: string }

const skills = shallowRef<SkillEntry[]>([])
const commands = shallowRef<CommandEntry[]>([])
const memory = shallowRef<MemoryEntry[]>([])
const engineVersion = ref<string | null>(null)
const builtinsMayBeStale = ref(false)
const scopeLabel = ref<string | null>(null)
const isLoading = ref(false)
const error = ref<string | null>(null)

// The scope (spawner id) the current data was loaded for. undefined = default.
let loadedSpawnerId: string | undefined
let hasLoaded = false
// Monotonic token so a slow in-flight load for an old scope can't overwrite a
// newer one (last-requested-wins).
let loadToken = 0

async function fetchJSON<T>(url: string): Promise<T> {
  const res = await fetch(url)
  if (!res.ok)
    throw new Error(`${url}: HTTP ${res.status}`)
  return await res.json() as T
}

function withSpawner(path: string, spawnerId?: string): string {
  return spawnerId ? `${path}?spawnerId=${encodeURIComponent(spawnerId)}` : path
}

// loadFile fetches a single editable config file's content for the active scope.
async function loadFile(path: string, spawnerId?: string): Promise<ConfigFile> {
  const params = new URLSearchParams({ path })
  if (spawnerId)
    params.set('spawnerId', spawnerId)
  return await fetchJSON<ConfigFile>(`/api/config/file?${params.toString()}`)
}

// saveFile writes content back, passing the loaded mtime for optimistic
// concurrency. Throws ConflictError on 409 (stale base), Error otherwise.
async function saveFile(path: string, content: string, baseMtime: number, spawnerId?: string): Promise<SaveResult> {
  const res = await fetch(withSpawner('/api/config/file', spawnerId), {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path, content, baseMtime }),
  })
  if (res.status === 409)
    throw new ConflictError()
  if (!res.ok)
    throw new Error(`save: HTTP ${res.status}`)
  return await res.json() as SaveResult
}

async function loadAll(spawnerId?: string): Promise<void> {
  const token = ++loadToken
  isLoading.value = true
  error.value = null
  try {
    const [s, c, m] = await Promise.all([
      fetchJSON<SkillsResponse>(withSpawner('/api/config/skills', spawnerId)),
      fetchJSON<CommandsResponse>(withSpawner('/api/config/commands', spawnerId)),
      fetchJSON<MemoryResponse>(withSpawner('/api/config/memory', spawnerId)),
    ])
    if (token !== loadToken)
      return // a newer load superseded this one
    skills.value = s.skills ?? []
    commands.value = c.commands ?? []
    memory.value = m.memory ?? []
    engineVersion.value = c.engineVersion ?? null
    builtinsMayBeStale.value = !!c.builtinsMayBeStale
    scopeLabel.value = c.scopeLabel ?? s.scopeLabel ?? null
    loadedSpawnerId = spawnerId
    hasLoaded = true
  }
  catch (err) {
    if (token === loadToken)
      error.value = (err as Error).message
  }
  finally {
    if (token === loadToken)
      isLoading.value = false
  }
}

export function useConfigExplorer() {
  // Initial load for the default scope if nothing has been fetched yet.
  if (!hasLoaded && !isLoading.value)
    void loadAll(undefined)

  // setSpawner switches the enumeration scope; no-op if already on it.
  function setSpawner(spawnerId?: string): void {
    if (hasLoaded && spawnerId === loadedSpawnerId)
      return
    void loadAll(spawnerId)
  }

  async function refresh(): Promise<void> {
    await loadAll(loadedSpawnerId)
  }

  return {
    skills,
    commands,
    memory,
    engineVersion,
    builtinsMayBeStale,
    scopeLabel,
    isLoading,
    error,
    refresh,
    setSpawner,
    loadFile,
    saveFile,
  }
}
