const DB_NAME = 'agent-dashboard'
const STORE = 'pending-messages'
const DB_VERSION = 1

export interface PendingMessage {
  id?: number
  agentPid: number
  sessionId: string
  message: string
  timestamp: number
  useChannel: boolean
  cwd?: string
}

let dbPromise: Promise<IDBDatabase> | null = null

export function openDB(): Promise<IDBDatabase> {
  if (dbPromise)
    return dbPromise

  dbPromise = new Promise((resolve, reject) => {
    const request = indexedDB.open(DB_NAME, DB_VERSION)

    request.onupgradeneeded = (event) => {
      const db = (event.target as IDBOpenDBRequest).result
      if (!db.objectStoreNames.contains(STORE)) {
        db.createObjectStore(STORE, { keyPath: 'id', autoIncrement: true })
      }
    }

    request.onsuccess = () => resolve(request.result)
    request.onerror = () => {
      dbPromise = null
      reject(request.error)
    }
  })

  return dbPromise
}

export async function addPending(msg: Omit<PendingMessage, 'id'>): Promise<void> {
  const db = await openDB()
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE, 'readwrite')
    const req = tx.objectStore(STORE).add(msg)
    req.onsuccess = () => resolve()
    req.onerror = () => reject(req.error)
  })
}

export async function getAllPending(): Promise<PendingMessage[]> {
  const db = await openDB()
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE, 'readonly')
    const req = tx.objectStore(STORE).getAll()
    req.onsuccess = () => resolve(req.result as PendingMessage[])
    req.onerror = () => reject(req.error)
  })
}

export async function removePending(id: number): Promise<void> {
  const db = await openDB()
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE, 'readwrite')
    const req = tx.objectStore(STORE).delete(id)
    req.onsuccess = () => resolve()
    req.onerror = () => reject(req.error)
  })
}

export async function countPending(): Promise<number> {
  const db = await openDB()
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE, 'readonly')
    const req = tx.objectStore(STORE).count()
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error)
  })
}

/**
 * Attempt to replay all pending messages.
 * Successful sends are removed from the queue; failed ones remain for retry.
 * Returns the number of messages successfully replayed.
 */
export async function replayPending(): Promise<number> {
  let replayed = 0
  const pending = await getAllPending()

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
          await removePending(msg.id)
        replayed++
      }
    }
    catch {
      // Network still unavailable — leave in queue for next attempt
    }
  }

  return replayed
}
