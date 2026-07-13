import { ref } from 'vue'

interface AgentIdentity {
  color: string
  emoji: string
  /** Short text label for screenreaders and non-emoji contexts, e.g. "Agent 3f" */
  identityLabel: string
}

const STORAGE_KEY = 'agent-identities'

// Tableau Color Blind 10 compatible palette — safe for deuteranopia/protanopia
export const COLORS = ['#4e79a7', '#f28e2b', '#e15759', '#76b7b2', '#59a14f', '#edc948', '#b07aa1', '#ff9da7']
export const EMOJIS = ['🤖', '🦾', '🧠', '⚡', '🔬', '🛠️', '🎯', '🚀', '🦊', '🐙']

function load(): Record<string, AgentIdentity> {
  try {
    const parsed = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? '{}')
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed))
      return parsed as Record<string, AgentIdentity>
    return {}
  }
  catch {
    return {}
  }
}

function persist(store: Record<string, AgentIdentity>): void {
  const data = JSON.stringify(store)
  // The write is deferred, so it can fire after the JS context is torn down
  // (e.g. a test's jsdom environment) — guard against a missing localStorage.
  const write = (): void => {
    if (typeof localStorage !== 'undefined')
      localStorage.setItem(STORAGE_KEY, data)
  }
  if (typeof requestIdleCallback !== 'undefined')
    requestIdleCallback(write)
  else
    setTimeout(write, 0)
}

function deterministicIndex(str: string, len: number): number {
  let hash = 0
  for (const ch of str)
    hash = ((hash << 5) - hash + ch.charCodeAt(0)) | 0
  return Math.abs(hash) % len
}

/** Returns a short hex suffix derived from the string (last 4 hex chars of hash) */
function hexSuffix(str: string): string {
  let hash = 0
  for (const ch of str)
    hash = ((hash << 5) - hash + ch.charCodeAt(0)) | 0
  return (hash >>> 0).toString(16).slice(-4)
}

function loadAndBackfill(): Record<string, AgentIdentity> {
  const store = load()
  let dirty = false
  for (const [path, entry] of Object.entries(store)) {
    if (!entry.identityLabel) {
      entry.identityLabel = `Agent ${hexSuffix(path)}`
      dirty = true
    }
  }
  if (dirty)
    persist(store)
  return store
}

const identities = ref<Record<string, AgentIdentity>>(loadAndBackfill())

export function useAgentIdentity() {
  function getIdentity(projectPath: string): AgentIdentity {
    if (!identities.value[projectPath]) {
      const emoji = EMOJIS[deterministicIndex(`${projectPath}!`, EMOJIS.length)]
      identities.value[projectPath] = {
        color: COLORS[deterministicIndex(projectPath, COLORS.length)],
        emoji,
        identityLabel: `Agent ${hexSuffix(projectPath)}`,
      }
      persist(identities.value)
    }
    return identities.value[projectPath]
  }

  function setIdentity(projectPath: string, identity: Partial<AgentIdentity>): void {
    identities.value[projectPath] = { ...getIdentity(projectPath), ...identity }
    persist(identities.value)
  }

  return { getIdentity, setIdentity }
}
