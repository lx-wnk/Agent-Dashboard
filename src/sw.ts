/// <reference lib="webworker" />

import type { PendingMessage } from './utils/pendingMessages'
import { DB_NAME, DB_VERSION, STORE } from './utils/pendingMessages'
import { parsePushNotice } from './utils/pushPayload'
import { BACKGROUND_SYNC_TAG, isWorkboxCache, SW_MSG_MESSAGES_REPLAYED, SW_MSG_SKIP_WAITING } from './utils/swConstants'

// __WB_MANIFEST was typed by workbox-precaching, which is no longer imported.
declare const self: ServiceWorkerGlobalScope & {
  __WB_MANIFEST: unknown
  __unusedPrecacheManifest: unknown
}

// Nothing here precaches. Push, background sync and skip-waiting below need no
// cache, and the one thing a cache bought — a shell while the server is down —
// could never render anything, because the API and the stream are down with it.
// What it cost was real: a previous build's index.html served against a
// restarted server that answers 404 for the bundle that document names, while
// the status bar reported the app up to date.
//
// injectManifest aborts the build unless it finds this token, and it searches
// the BUNDLED worker, where a bare expression statement is dropped as dead
// code. Assigning it to a global is a side effect the bundler has to keep.
// Nothing ever reads the property.
self.__unusedPrecacheManifest = self.__WB_MANIFEST

// Take over as soon as this worker installs. registerType is 'prompt' so a new
// worker never swaps precached assets under a running session — but this one
// precaches nothing, so there is nothing to swap, and waiting is what kept the
// eviction below from ever running: the previous worker holds control, the new
// one sits in "waiting", and the stale cache is served on indefinitely.
self.addEventListener('install', () => {
  void self.skipWaiting()
})

// An install that already carries a precache keeps being served from it until
// the cache is gone, so the first worker without precaching has to take it out.
self.addEventListener('activate', (event) => {
  event.waitUntil((async () => {
    const keys = await caches.keys()
    await Promise.all(keys.filter(isWorkboxCache).map(k => caches.delete(k)))
    await self.clients.claim()
  })())
})

// ---------- IndexedDB helpers (inlined — SW cannot import ES modules at runtime) ----------

let _db: IDBDatabase | null = null

function openIDB(): Promise<IDBDatabase> {
  if (_db !== null)
    return Promise.resolve(_db)
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, DB_VERSION)
    req.onupgradeneeded = (e) => {
      const db = (e.target as IDBOpenDBRequest).result
      if (!db.objectStoreNames.contains(STORE))
        db.createObjectStore(STORE, { keyPath: 'id', autoIncrement: true })
    }
    req.onsuccess = () => {
      _db = req.result
      resolve(_db)
    }
    req.onerror = () => reject(req.error)
  })
}

async function getAllPendingIDB(): Promise<PendingMessage[]> {
  const db = await openIDB()
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE, 'readonly')
    const req = tx.objectStore(STORE).getAll()
    req.onsuccess = () => resolve(req.result as PendingMessage[])
    req.onerror = () => reject(req.error)
  })
}

async function removePendingIDB(id: number): Promise<void> {
  const db = await openIDB()
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE, 'readwrite')
    const req = tx.objectStore(STORE).delete(id)
    req.onsuccess = () => resolve()
    req.onerror = () => reject(req.error)
  })
}

// ---------- Background Sync handler ----------

async function replayPendingMessages(): Promise<void> {
  const pending = await getAllPendingIDB()
  if (pending.length === 0)
    return

  let anyFailed = false
  let replayed = 0
  for (const msg of pending) {
    try {
      let res: Response
      if (msg.useChannel) {
        res = await fetch(`/api/agents/${msg.sessionId}/message`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ message: msg.message }),
        })
      }
      else {
        res = await fetch('/api/agents/spawn', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            prompt: msg.message,
            cwd: msg.cwd,
            resumeSessionId: msg.sessionId,
          }),
        })
      }

      if (res.ok) {
        if (msg.id !== undefined)
          await removePendingIDB(msg.id)
        replayed++
      }
      else if (res.status >= 400 && res.status < 500) {
        // Permanent failure — remove from IDB, notify main thread
        if (msg.id !== undefined)
          await removePendingIDB(msg.id)
        self.clients.matchAll().then((clients) => {
          clients.forEach(c => c.postMessage({
            type: 'OFFLINE_MESSAGE_FAILED',
            messageId: msg.id,
            status: res.status,
          }))
        })
        continue // don't set anyFailed, move to next message
      }
      else {
        anyFailed = true // transient failure — signal retry
      }
    }
    catch {
      anyFailed = true
    }
  }

  if (replayed > 0) {
    const clients = await self.clients.matchAll()
    clients.forEach(c => c.postMessage({ type: SW_MSG_MESSAGES_REPLAYED, count: replayed }))
  }

  if (anyFailed) {
    // Re-throw to tell the browser to retry the sync later
    throw new Error('Some messages could not be delivered — will retry')
  }
}

// The Background Sync API's SyncEvent is absent from TS's ServiceWorkerGlobalScopeEventMap,
// so the listener param falls back to Event — model it as ExtendableEvent plus the sync tag.
self.addEventListener('sync', (event) => {
  const syncEvent = event as ExtendableEvent & { readonly tag: string }
  if (syncEvent.tag === BACKGROUND_SYNC_TAG) {
    syncEvent.waitUntil(replayPendingMessages())
  }
})

// Activate when refreshServiceWorker asks a waiting worker to take over.
// Without this handler the waiting worker never takes control and prompt-mode
// updates can never apply — the stale precached bundle is served indefinitely.
self.addEventListener('message', (event) => {
  if (event.data?.type === SW_MSG_SKIP_WAITING)
    self.skipWaiting()
})

self.addEventListener('push', (event) => {
  let raw: unknown = null
  try {
    raw = event.data?.json()
  }
  catch {
    raw = null
  }
  const notice = parsePushNotice(raw)
  if (!notice)
    return
  event.waitUntil(self.registration.showNotification(notice.title, {
    body: notice.body,
    tag: notice.tag,
    data: { url: notice.url },
  }))
})

self.addEventListener('notificationclick', (event) => {
  event.notification.close()
  const url = (event.notification.data as { url?: string } | undefined)?.url ?? '/'
  event.waitUntil((async () => {
    const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
    const open = windows.find(w => 'focus' in w)
    if (open)
      return (open as WindowClient).focus()
    return self.clients.openWindow(url)
  })())
})
