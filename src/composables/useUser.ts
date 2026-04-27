import { readonly, ref } from 'vue'

export interface DashboardUser {
  id: string
  login: string
  isAdmin: boolean
}

const user = ref<DashboardUser | null>(null)
const isAdmin = ref(true) // default true for standalone
const authEnabled = ref(false)
const loaded = ref(false)

async function loadUser(): Promise<void> {
  try {
    const res = await fetch('/api/me')
    if (!res.ok) {
      authEnabled.value = true
      user.value = null
      return
    }
    const data = await res.json() as { user: DashboardUser | null, isAdmin: boolean, authEnabled: boolean }
    user.value = data.user
    isAdmin.value = data.isAdmin
    authEnabled.value = data.authEnabled
  }
  catch {
    // network error — assume standalone
  }
  finally {
    loaded.value = true
  }
}

export function useUser() {
  return {
    user: readonly(user),
    isAdmin: readonly(isAdmin),
    authEnabled: readonly(authEnabled),
    loaded: readonly(loaded),
    loadUser,
  }
}
