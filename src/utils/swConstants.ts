export const BACKGROUND_SYNC_TAG = 'replay-agent-messages'
export const SW_MSG_MESSAGES_REPLAYED = 'MESSAGES_REPLAYED'
// Posted by the page (usePWA.updateSW) to tell a waiting service worker to
// activate immediately; the SW must handle it or prompt-mode updates never apply.
export const SW_MSG_SKIP_WAITING = 'SKIP_WAITING'

// Workbox names every cache it creates `workbox-*` or `<prefix>-precache-*`.
// Both the page (evicting a desktop-shell install) and the worker (evicting a
// precache left by an older build) have to recognise them, and CacheStorage is
// origin-scoped, so a blanket delete would also take any cache a future
// feature adds here.
export function isWorkboxCache(name: string): boolean {
  return name.startsWith('workbox-') || name.includes('-precache-') || name.includes('-runtime-')
}
