import type express from 'express'
import { Router } from 'express'
import { deletePresetsForProject, listPresetProjectSummaries } from '../db/permissionPresetsRepo.js'

type RejectCrossOrigin = (req: express.Request, res: express.Response) => boolean

export function createPresetRouter(rejectCrossOrigin: RejectCrossOrigin): Router {
  const router = Router()

  router.get('/settings/permission-presets', (req, res) => {
    if (rejectCrossOrigin(req, res))
      return
    const userId = (req as express.Request & { user?: { id: string } }).user?.id ?? null
    const summaries = listPresetProjectSummaries(userId)
    res.json(summaries)
  })

  const mutationRouter = Router()
  mutationRouter.use((req, res, next) => {
    if (rejectCrossOrigin(req, res))
      return
    next()
  })

  mutationRouter.delete('/settings/permission-presets', (req, res) => {
    const userId = (req as express.Request & { user?: { id: string } }).user?.id ?? null
    const body = req.body as { cwd?: unknown }
    if (typeof body.cwd !== 'string' || !body.cwd.trim()) {
      res.status(400).json({ error: 'cwd is required' })
      return
    }
    deletePresetsForProject(userId, body.cwd.trim())
    res.json({ ok: true })
  })

  router.use(mutationRouter)
  return router
}
