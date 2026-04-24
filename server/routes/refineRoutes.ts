import type express from 'express'
import type { Buffer } from 'node:buffer'
import { Router } from 'express'
import { appendAudit } from '../db/auditRepo.js'
import { insertTurn, listTurns } from '../db/refinementTurnsRepo.js'
import { getTaskById, updateTask } from '../db/tasksRepo.js'
import { spawnRefinementTurn } from '../pipeline/refinementSpawner.js'
import { bulkGrantKonzeptPermissions } from '../services/approvalUtils.js'

type RejectCrossOrigin = (req: express.Request, res: express.Response) => boolean

const PHASE_DONE_RE = /__phase_done:\s*(\w+)/
const JSON_BLOCK_RE = /```json\n([\s\S]*?)```/

export function createRefineRouter(
  broadcastEnrichedUpdate: (taskId: string) => void,
  rejectCrossOrigin: RejectCrossOrigin,
): Router {
  const router = Router()

  // CSRF guard applied as sub-router middleware so every POST/PUT/PATCH/DELETE
  // route registered below is protected without per-handler boilerplate.
  const mutationRouter = Router()
  mutationRouter.use((req, res, next) => {
    if (rejectCrossOrigin(req, res))
      return
    next()
  })

  mutationRouter.post('/:taskId/turn', async (req, res) => {
    const task = getTaskById(req.params.taskId)
    if (!task || task.currentStage !== 'konzept') {
      res.status(404).json({ error: 'Task not found or not in konzept stage' })
      return
    }

    const body = req.body as { message?: unknown }
    const message = typeof body.message === 'string' ? body.message.trim() : ''
    if (!message) {
      res.status(400).json({ error: 'message is required' })
      return
    }

    const history = listTurns(req.params.taskId)
    insertTurn({ taskId: task.id, role: 'user', content: message })

    res.setHeader('Content-Type', 'text/event-stream')
    res.setHeader('Cache-Control', 'no-cache')
    res.setHeader('Connection', 'keep-alive')
    res.flushHeaders()

    const { stdout, waitForExit } = spawnRefinementTurn(message, history, task.cwd)

    let fullResponse = ''
    stdout.on('data', (chunk: Buffer) => {
      const text = chunk.toString()
      fullResponse += text
      res.write(`data: ${JSON.stringify({ text })}\n\n`)
    })

    stdout.on('error', (streamErr) => {
      insertTurn({ taskId: task.id, role: 'assistant', content: fullResponse || '[stream error]' })
      res.write(`event: error\ndata: ${JSON.stringify({ error: String(streamErr) })}\n\n`)
      res.end()
    })

    try {
      await waitForExit()

      const phaseMatch = fullResponse.match(PHASE_DONE_RE)
      const detectedPhase = phaseMatch ? phaseMatch[1] : undefined

      insertTurn({
        taskId: task.id,
        role: 'assistant',
        content: fullResponse,
        ...(detectedPhase ? { phase: detectedPhase } : {}),
      })

      if (detectedPhase) {
        res.write(`event: phase_change\ndata: ${JSON.stringify({ phase: detectedPhase })}\n\n`)
      }
      res.write(`event: done\ndata: {}\n\n`)
    }
    catch (err) {
      insertTurn({ taskId: task.id, role: 'assistant', content: fullResponse || '[error]' })
      res.write(`event: error\ndata: ${JSON.stringify({ error: err instanceof Error ? err.message : 'spawn failed' })}\n\n`)
    }
    res.end()
  })

  mutationRouter.post('/:taskId/confirm', (req, res) => {
    const task = getTaskById(req.params.taskId)
    if (!task || task.currentStage !== 'konzept') {
      res.status(404).json({ error: 'Task not found or not in konzept stage' })
      return
    }

    const turns = listTurns(req.params.taskId)
    const lastAssistant = [...turns].reverse().find(t => t.role === 'assistant')
    if (!lastAssistant) {
      res.status(409).json({ error: 'No assistant message found' })
      return
    }

    const jsonMatch = lastAssistant.content.match(JSON_BLOCK_RE)
    if (!jsonMatch) {
      res.status(409).json({ error: 'No JSON block found in last assistant message' })
      return
    }

    let konzeptOutput: Record<string, unknown>
    try {
      konzeptOutput = JSON.parse(jsonMatch[1])
    }
    catch {
      res.status(409).json({ error: 'Invalid JSON in assistant output' })
      return
    }

    if (typeof konzeptOutput !== 'object' || konzeptOutput === null || Array.isArray(konzeptOutput)) {
      res.status(409).json({ error: 'Assistant JSON is not a plain object' })
      return
    }

    updateTask(task.id, {
      title: typeof konzeptOutput.refinedTitle === 'string' ? konzeptOutput.refinedTitle : task.title,
      description: typeof konzeptOutput.refinedDescription === 'string'
        ? konzeptOutput.refinedDescription
        : task.description,
      cwd: typeof konzeptOutput.cwd === 'string' ? konzeptOutput.cwd : task.cwd,
      metadata: { ...(task.metadata ?? {}), konzeptOutput },
      currentStage: 'backlog',
    })

    bulkGrantKonzeptPermissions(task.id)
    broadcastEnrichedUpdate(task.id)
    appendAudit({ taskId: task.id, actor: 'user', action: 'refine_confirmed', details: { cwd: konzeptOutput.cwd } })

    res.json(getTaskById(task.id))
  })

  router.get('/:taskId/turns', (req, res) => {
    const task = getTaskById(req.params.taskId)
    if (!task) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    res.json(listTurns(req.params.taskId))
  })

  // Mount mutation sub-router so its rejectCrossOrigin middleware guards
  // every POST route registered above.
  router.use(mutationRouter)

  return router
}
