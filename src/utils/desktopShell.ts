/**
 * The desktop shell and a normal browser are served the same SPA from the same
 * origin (the in-process server on 127.0.0.1:13120), so the shell announces
 * itself in the URL its bootstrap page redirects to. The flag is mirrored into
 * sessionStorage because in-app navigation can drop the query string.
 */
export const DESKTOP_SHELL_PARAM = 'shell'
export const DESKTOP_SHELL_VALUE = 'desktop'

const STORAGE_KEY = 'agent-dashboard-desktop-shell'

export function isDesktopShell(search: string = typeof location === 'undefined' ? '' : location.search): boolean {
  if (new URLSearchParams(search).get(DESKTOP_SHELL_PARAM) === DESKTOP_SHELL_VALUE) {
    try {
      sessionStorage.setItem(STORAGE_KEY, 'true')
    }
    catch {
      // Private mode / storage disabled — the query string still carries the flag.
    }
    return true
  }
  try {
    return sessionStorage.getItem(STORAGE_KEY) === 'true'
  }
  catch {
    return false
  }
}
