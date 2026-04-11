import { ref, watch } from 'vue'

export type Theme = 'dark' | 'light'

const theme = ref<Theme>('dark')
let initialized = false

function initTheme() {
  if (initialized) return
  initialized = true
  const stored = localStorage.getItem('agent-theme')
  const resolved: Theme = stored === 'light' || stored === 'dark'
    ? stored
    : (window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark')
  theme.value = resolved
  document.documentElement.setAttribute('data-theme', resolved)

  watch(theme, (t) => {
    document.documentElement.setAttribute('data-theme', t)
    localStorage.setItem('agent-theme', t)
  })
}

export function useTheme() {
  initTheme()

  function toggleTheme() {
    theme.value = theme.value === 'dark' ? 'light' : 'dark'
  }

  return { theme, toggleTheme }
}
