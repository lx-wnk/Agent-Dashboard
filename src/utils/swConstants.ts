export const BACKGROUND_SYNC_TAG = 'replay-agent-messages'
export const SW_MSG_MESSAGES_REPLAYED = 'MESSAGES_REPLAYED'
// Posted by refreshServiceWorker to tell a waiting worker to activate at the
// one moment the page reloads deliberately, after a server restart.
export const SW_MSG_SKIP_WAITING = 'SKIP_WAITING'

// Workbox names every cache it creates `workbox-*` or `<prefix>-precache-*`.
// Both the page (evicting a desktop-shell install) and the worker (evicting a
// precache left by an older build) have to recognise them, and CacheStorage is
// origin-scoped, so a blanket delete would also take any cache a future
// feature adds here.
export function isWorkboxCache(name: string): boolean {
  return name.startsWith('workbox-') || name.includes('-precache-') || name.includes('-runtime-')
}
