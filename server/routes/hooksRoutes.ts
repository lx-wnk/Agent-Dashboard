import type { Router } from 'express'
import { randomUUID } from 'node:crypto'
import express from 'express'

export interface HooksRouterOptions {
  /** Callback invoked for every authenticated incoming hook event. */
  onEvent: () => void
  /**
   * Shared secret. When non-empty, incoming requests must supply
   * `Authorization: Bearer <secret>`. When empty, all POSTs from localhost
   * are accepted.
   */
  secret: string
}

interface PendingEdit {
  sessionId: string
  toolName: string
  filePath: string
  oldContent: string
  newContent: string
  createdAt: number
  decision: 'pending' | 'accept' | 'reject'
}

const EDIT_GATE_TIMEOUT_MS = 30_000
const EDIT_GATE_POLL_MS = 500
const pendingEdits = new Map<string, PendingEdit>()

export function createHooksRouter(opts: HooksRouterOptions): Router {
  const router = express.Router()

  router.post('/event', (req, res) => {
    if (opts.secret) {
      const auth = req.headers.authorization ?? ''
      if (auth !== `Bearer ${opts.secret}`) {
        res.status(401).end()
        return
      }
    }

    // Acknowledge immediately — the hook script has a 500ms timeout
    res.status(204).end()

    // Trigger debounced SSE rescan without blocking the response
    opts.onEvent()
  })

  // ─── Edit Gate ─────────────────────────────────────────────────────────────

  router.post('/pre-tool', (req, res) => {
    const { sessionId, toolName, filePath, oldContent, newContent } = req.body as {
      sessionId: string
      toolName: string
      filePath: string
      oldContent: string
      newContent: string
    }

    if (!['Edit', 'Write', 'MultiEdit'].includes(toolName)) {
      res.json({ proceed: true })
      return
    }

    const id = randomUUID()
    const edit: PendingEdit = {
      sessionId,
      toolName,
      filePath,
      oldContent: oldContent ?? '',
      newContent,
      createdAt: Date.now(),
      decision: 'pending',
    }
    pendingEdits.set(id, edit)

    const deadline = Date.now() + EDIT_GATE_TIMEOUT_MS
    const interval = setInterval(() => {
      const e = pendingEdits.get(id)
      if (!e) {
        clearInterval(interval)
        res.json({ proceed: true })
        return
      }
      if (e.decision !== 'pending') {
        clearInterval(interval)
        pendingEdits.delete(id)
        res.json({ proceed: e.decision === 'accept' })
        return
      }
      if (Date.now() >= deadline) {
        clearInterval(interval)
        pendingEdits.delete(id)
        res.json({ proceed: true })
      }
    }, EDIT_GATE_POLL_MS)
  })

  router.post('/respond', (req, res) => {
    const { id, decision } = req.body as { id: string, decision: 'accept' | 'reject' }
    const edit = pendingEdits.get(id)
    if (!edit) {
      res.status(404).json({ error: 'No pending edit with that id' })
      return
    }
    edit.decision = decision
    res.json({ ok: true })
  })

  router.get('/pending', (req, res) => {
    const sessionId = req.query.sessionId as string | undefined
    const edits = [...pendingEdits.entries()]
      .filter(([, e]) => !sessionId || e.sessionId === sessionId)
      .map(([id, e]) => ({ id, ...e }))
    res.json({ edits })
  })

  return router
}
