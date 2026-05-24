import { ref } from 'vue'

export interface SessionInfo {
  sessionId: string
  projectPath: string
  projectName: string
  lastModified: string
  model: string | null
  firstPrompt: string | null
  lastResponse: string | null
  totalInputTokens: number
  totalOutputTokens: number
  costEstimate: number
  isRunning: boolean
}

const sessions = ref<SessionInfo[]>([])
const loading = ref(false)
const error = ref<string | null>(null)

async function fetchSessions() {
  loading.value = true
  error.value = null
  try {
    const res = await fetch('/api/sessions')
    if (res.ok)
      sessions.value = await res.json()
    else
      error.value = `Failed to load sessions (${res.status})`
  }
  catch {
    error.value = 'Network error loading sessions.'
  }
  finally {
    loading.value = false
  }
}

export function useSessions() {
  return {
    sessions,
    loading,
    error,
    refetch: fetchSessions,
  }
}
