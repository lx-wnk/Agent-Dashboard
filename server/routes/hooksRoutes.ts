import type { Router } from 'express'
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

  return router
}
