import { ref, watch } from 'vue'

export type ThemePreference = 'dark' | 'light' | 'system'
export type Theme = 'dark' | 'light'

const preference = ref<ThemePreference>('system')
const theme = ref<Theme>('dark')
let initialized = false

function resolveTheme(pref: ThemePreference): Theme {
  if (pref === 'system')
    return window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark'
  return pref
}

function applyTheme(t: Theme) {
  document.documentElement.classList.toggle('dark', t === 'dark')
}

function initTheme() {
  if (initialized)
    return
  initialized = true

  const stored = localStorage.getItem('agent-theme') as ThemePreference | null
  preference.value = stored === 'light' || stored === 'dark' || stored === 'system'
    ? stored
    : 'system'
  theme.value = resolveTheme(preference.value)
  applyTheme(theme.value)

  // Respond to OS preference changes while in system mode
  window.matchMedia('(prefers-color-scheme: light)').addEventListener('change', () => {
    if (preference.value === 'system') {
      theme.value = resolveTheme('system')
      applyTheme(theme.value)
    }
  })

  watch(preference, (pref) => {
    theme.value = resolveTheme(pref)
    applyTheme(theme.value)
    localStorage.setItem('agent-theme', pref)
  })
}

export function useTheme() {
  initTheme()

  function setTheme(pref: ThemePreference) {
    preference.value = pref
  }

  function toggleTheme() {
    preference.value = preference.value === 'dark' ? 'light' : 'dark'
  }

  return { theme, preference, setTheme, toggleTheme }
}
