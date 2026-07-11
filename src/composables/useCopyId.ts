import type { MaybeRefOrGetter } from 'vue'
import { useClipboard } from '@vueuse/core'
import { toValue } from 'vue'

export function shortId(id: string): string {
  return id.slice(0, 8)
}

/**
 * `legacy: true` falls back to `document.execCommand('copy')` when the async
 * Clipboard API is unavailable or denied (e.g. gesture/permission-restricted
 * WKWebView contexts), so copy actions degrade gracefully instead of failing.
 * `copiedDuring: 2000` matches the 2s "copied!" flash used by sibling
 * copy buttons across the app.
 */
export function useClipboardCopy() {
  return useClipboard({ legacy: true, copiedDuring: 2000 })
}

export function useCopyId(id: MaybeRefOrGetter<string>) {
  const { copy, copied } = useClipboard({ legacy: true })
  return { copy: () => copy(toValue(id)), copied }
}
