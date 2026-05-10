import { ref } from 'vue'

interface AgentIdentity {
  color: string
  emoji: string
}

const STORAGE_KEY = 'agent-identities'

const COLORS = ['#3b82f6', '#8b5cf6', '#10b981', '#f59e0b', '#ef4444', '#06b6d4', '#f97316', '#84cc16']
const EMOJIS = ['🤖', '🦾', '🧠', '⚡', '🔬', '🛠️', '🎯', '🚀', '🦊', '🐙']

function load(): Record<string, AgentIdentity> {
  try {
    return JSON.parse(localStorage.getItem(STORAGE_KEY) ?? '{}')
  }
  catch {
    return {}
  }
}

function persist(store: Record<string, AgentIdentity>): void {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(store))
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
