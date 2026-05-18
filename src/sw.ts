/// <reference lib="webworker" />
import { cleanupOutdatedCaches, precacheAndRoute } from 'workbox-precaching'
import { BACKGROUND_SYNC_TAG, SW_MSG_MESSAGES_REPLAYED } from './utils/swConstants'
import { DB_NAME, DB_VERSION, STORE } from './utils/pendingMessages'
import type { PendingMessage } from './utils/pendingMessages'

declare const self: ServiceWorkerGlobalScope

// Workbox precache manifest injected at build time by vite-plugin-pwa
precacheAndRoute(self.__WB_MANIFEST)
cleanupOutdatedCaches()

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

self.addEventListener('sync', (event) => {
  if (event.tag === BACKGROUND_SYNC_TAG) {
    event.waitUntil(replayPendingMessages())
  }
})
