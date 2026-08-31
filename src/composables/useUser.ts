import { readonly, ref } from 'vue'

export interface DashboardUser {
  id: string
  login: string
}

const user = ref<DashboardUser | null>(null)
const authEnabled = ref(false)
const loaded = ref(false)

let retryTimer: ReturnType<typeof setTimeout> | null = null

function scheduleRetry() {
  if (retryTimer)
    return
  retryTimer = setTimeout(() => {
    retryTimer = null
    loadUser()
  }, 2000)
}

async function loadUser(): Promise<void> {
  try {
    const res = await fetch('/api/me')
    if (!res.ok) {
      authEnabled.value = true
      user.value = null
      scheduleRetry() // server may still be starting up
      return
    }
    if (retryTimer) {
      clearTimeout(retryTimer)
      retryTimer = null
    }
    const data = await res.json() as { user: DashboardUser | null, authEnabled: boolean }
    user.value = data.user
    authEnabled.value = data.authEnabled
  }
  catch {
    scheduleRetry() // network error — retry until server is up
  }
  finally {
    loaded.value = true
  }
}

export function useUser() {
  return {
    user: readonly(user),
    authEnabled: readonly(authEnabled),
    loaded: readonly(loaded),
    loadUser,
  }
}
