import { ref, shallowRef } from 'vue'
import { errorMessage } from '../utils/errorMessage'

export interface SkillEntry {
  name: string
  source: string
  description: string
}

export interface CommandEntry {
  name: string
  source: string
  description: string
  body: string
}

export interface MemoryEntry {
  path: string
  scope: 'user' | 'project'
  size: number
  mtime: number
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
      error.value = errorMessage(err)
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
  }
}
