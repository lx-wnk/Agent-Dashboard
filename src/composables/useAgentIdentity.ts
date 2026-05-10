import { ref } from 'vue'

interface AgentIdentity {
  color: string
  emoji: string
}

const STORAGE_KEY = 'agent-identities'

// Tableau Color Blind 10 compatible palette — safe for deuteranopia/protanopia
export const COLORS = ['#4e79a7', '#f28e2b', '#e15759', '#76b7b2', '#59a14f', '#edc948', '#b07aa1', '#ff9da7']
export const EMOJIS = ['🤖', '🦾', '🧠', '⚡', '🔬', '🛠️', '🎯', '🚀', '🦊', '🐙']

function load(): Record<string, AgentIdentity> {
  try {
    return JSON.parse(localStorage.getItem(STORAGE_KEY) ?? '{}')
  }
  catch {
    return {}
  }
}

function persist(store: Record<string, AgentIdentity>): void {
  const data = JSON.stringify(store)
  if (typeof requestIdleCallback !== 'undefined') {
    requestIdleCallback(() => localStorage.setItem(STORAGE_KEY, data))
  }
  else {
    setTimeout(() => localStorage.setItem(STORAGE_KEY, data), 0)
  }
}

function deterministicIndex(str: string, len: number): number {
  let hash = 0
  for (const ch of str)
    hash = ((hash << 5) - hash + ch.charCodeAt(0)) | 0
  return Math.abs(hash) % len
}

const identities = ref<Record<string, AgentIdentity>>(load())

export function useAgentIdentity() {
  function getIdentity(projectPath: string): AgentIdentity {
    if (!identities.value[projectPath]) {
      identities.value[projectPath] = {
        color: COLORS[deterministicIndex(projectPath, COLORS.length)],
        emoji: EMOJIS[deterministicIndex(`${projectPath}!`, EMOJIS.length)],
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
