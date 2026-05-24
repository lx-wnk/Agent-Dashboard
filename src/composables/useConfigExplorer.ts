import { ref, shallowRef } from 'vue'

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

const skills = shallowRef<SkillEntry[]>([])
const commands = shallowRef<CommandEntry[]>([])
const memory = shallowRef<MemoryEntry[]>([])
const isLoading = ref(false)
const error = ref<string | null>(null)
let loaded = false
let inflight: Promise<void> | null = null

async function fetchJSON<T>(url: string): Promise<T> {
  const res = await fetch(url)
  if (!res.ok)
    throw new Error(`${url}: HTTP ${res.status}`)
  return await res.json() as T
}

async function loadAll(): Promise<void> {
  if (loaded)
    return
  if (inflight) {
    await inflight
    return
  }
  isLoading.value = true
  error.value = null
  inflight = (async () => {
    try {
      const [s, c, m] = await Promise.all([
        fetchJSON<SkillEntry[]>('/api/config/skills'),
        fetchJSON<CommandEntry[]>('/api/config/commands'),
        fetchJSON<MemoryEntry[]>('/api/config/memory'),
      ])
      skills.value = s ?? []
      commands.value = c ?? []
      memory.value = m ?? []
      loaded = true
    }
    catch (err) {
      error.value = (err as Error).message
    }
    finally {
      isLoading.value = false
      inflight = null
    }
  })()
  await inflight
}

export function useConfigExplorer() {
  if (!loaded && !inflight)
    void loadAll()

  async function refresh(): Promise<void> {
    loaded = false
    await loadAll()
  }

  return {
    skills,
    commands,
    memory,
    isLoading,
    error,
    refresh,
  }
}
