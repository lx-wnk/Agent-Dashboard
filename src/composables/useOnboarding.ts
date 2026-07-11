import { readonly, ref } from 'vue'

export interface OnboardingStatus {
  completed: boolean
  cliInstalled: boolean
  cliVersion: string
  mcpRegistered: boolean
}

export interface RegisterMcpResult {
  ok: boolean
  command: string
}

const status = ref<OnboardingStatus | null>(null)
const loading = ref(false)
const error = ref<string | null>(null)
const visible = ref(false)

async function fetchStatus(): Promise<void> {
  loading.value = true
  error.value = null
  try {
    const res = await fetch('/api/onboarding/status')
    if (!res.ok) {
      error.value = `Failed to load onboarding status (${res.status})`
      return
    }
    status.value = await res.json() as OnboardingStatus
  }
  catch {
    error.value = 'Network error loading onboarding status.'
  }
  finally {
    loading.value = false
  }
}

async function registerMcp(): Promise<RegisterMcpResult | null> {
  loading.value = true
  error.value = null
  try {
    const res = await fetch('/api/onboarding/register-mcp', { method: 'POST' })
    const data = await res.json() as RegisterMcpResult & { error?: string }
    if (!res.ok) {
      error.value = data.error ?? `Failed to connect the dashboard (${res.status})`
      return null
    }
    if (status.value)
      status.value = { ...status.value, mcpRegistered: data.ok }
    return data
  }
  catch {
    error.value = 'Network error connecting the dashboard.'
    return null
  }
  finally {
    loading.value = false
  }
}

async function complete(): Promise<boolean> {
  try {
    const res = await fetch('/api/onboarding/status', {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ completed: true }),
    })
    if (res.ok && status.value)
      status.value = { ...status.value, completed: true }
    return res.ok
  }
  catch {
    error.value = 'Network error saving onboarding status.'
    return false
  }
}

// Local session-only visibility, independent of the persisted `completed` flag —
// lets the user re-run the flow from Settings without server state implying it's incomplete.
function show(): void {
  visible.value = true
}

function hide(): void {
  visible.value = false
}

export function useOnboarding() {
  return {
    status: readonly(status),
    loading: readonly(loading),
    error: readonly(error),
    visible: readonly(visible),
    fetchStatus,
    registerMcp,
    complete,
    show,
    hide,
  }
}
