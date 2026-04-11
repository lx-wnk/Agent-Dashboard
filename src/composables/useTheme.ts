import { ref, watch } from 'vue'

export type Theme = 'dark' | 'light'

const stored = localStorage.getItem('agent-theme')
const defaultTheme: Theme = stored === 'light' || stored === 'dark'
  ? stored
  : (window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark')

const theme = ref<Theme>(defaultTheme)

// Apply immediately on load
document.documentElement.setAttribute('data-theme', theme.value)

watch(theme, (t) => {
  document.documentElement.setAttribute('data-theme', t)
  localStorage.setItem('agent-theme', t)
})

export function useTheme() {
  function toggleTheme() {
    theme.value = theme.value === 'dark' ? 'light' : 'dark'
  }

  return { theme, toggleTheme }
}
