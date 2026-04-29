import type express from 'express'
import { Buffer } from 'node:buffer'
import { unlinkSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import process from 'node:process'
import { Router } from 'express'
import { jsonrepair } from 'jsonrepair'
import { appendAudit } from '../db/auditRepo.js'
import { insertTurn, listTurns } from '../db/refinementTurnsRepo.js'
import { getTaskById, updateTask } from '../db/tasksRepo.js'
import { spawnRefinementTurn } from '../services/refinementSpawner.js'
import { applyPresetPermissions, bulkGrantKonzeptPermissions, saveGrantsToPresets } from '../services/approvalUtils.js'

interface ImageAttachment {
  dataUrl: string
  mimeType: string
}

type RejectCrossOrigin = (req: express.Request, res: express.Response) => boolean

const PHASE_DONE_RE = /(?:^|\n)__phase_done:\s*(\w+)\s*$/
const JSON_BLOCK_RE = /```json\n([\s\S]*?)```/g

const REFINEMENT_TIMEOUT_MS = Number(process.env.REFINEMENT_TIMEOUT_MS ?? '') || 5 * 60 * 1000

const activeTurns = new Set<string>()

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

    if (activeTurns.has(task.id)) {
      res.status(409).json({ error: 'A turn is already in progress for this task' })
      return
    }


    const body = req.body as { message?: unknown, images?: unknown }
    const message = typeof body.message === 'string' ? body.message.trim() : ''
    if (!message) {
      res.status(400).json({ error: 'message is required' })
      return
    }

    const rawImages = Array.isArray(body.images) ? body.images : []
    const images: ImageAttachment[] = rawImages.filter(
      (img): img is ImageAttachment =>
        typeof img === 'object' && img !== null
        && typeof (img as Record<string, unknown>).dataUrl === 'string'
        && typeof (img as Record<string, unknown>).mimeType === 'string',
    )

    const tempFiles: string[] = []
    let spawnMessage = message
    if (images.length > 0) {
      const paths: string[] = []
      for (const img of images) {
        const ext = img.mimeType.replace('image/', '').split('+')[0] || 'png'
        const filename = `refine-${Date.now()}-${Math.random().toString(36).slice(2)}.${ext}`
        const filePath = join(tmpdir(), filename)
        const b64 = img.dataUrl.includes(',') ? img.dataUrl.split(',')[1] : img.dataUrl
        writeFileSync(filePath, Buffer.from(b64, 'base64'))
        tempFiles.push(filePath)
        paths.push(filePath)
      }
      spawnMessage = `${message}\n\n[Attached image${paths.length > 1 ? 's' : ''} — please read and analyse: ${paths.join(', ')}]`
    }

    const history = listTurns(req.params.taskId)

    // Use first user message as provisional title while refinement is in progress
    if (history.filter(t => t.role === 'user').length === 0 && task.title === 'New Task') {
      const provisional = message.replace(/\s+/g, ' ').trim().slice(0, 60)
      updateTask(task.id, { title: provisional })
      broadcastEnrichedUpdate(task.id)
    }

    insertTurn({ taskId: task.id, role: 'user', content: message })

    activeTurns.add(task.id)
    res.setHeader('Content-Type', 'text/event-stream')
    res.setHeader('Cache-Control', 'no-cache')
    res.setHeader('Connection', 'keep-alive')
    res.flushHeaders()

    const { child, stdout, waitForExit, getStderr } = spawnRefinementTurn(spawnMessage, history, task.cwd)

    let fullResponse = ''
    let turnFinalized = false

    let guardTriggered = false

    const onClose = () => {
      if (guardTriggered || turnFinalized)
        return
      guardTriggered = true
      turnFinalized = true
      child.kill('SIGTERM')
      activeTurns.delete(task.id)
      insertTurn({ taskId: task.id, role: 'assistant', content: fullResponse || '[connection closed]' })
    }
    res.on('close', onClose)

    const timeoutHandle = setTimeout(() => {
      if (guardTriggered || turnFinalized)
        return
      guardTriggered = true
      turnFinalized = true
      child.kill('SIGTERM')
      activeTurns.delete(task.id)
      insertTurn({ taskId: task.id, role: 'assistant', content: fullResponse || '[timeout]' })
      res.write(`event: error\ndata: ${JSON.stringify({ error: 'refinement turn timed out' })}\n\n`)
      res.end()
    }, REFINEMENT_TIMEOUT_MS)

    stdout.on('data', (chunk: Buffer) => {
      if (turnFinalized)
        return
      const text = chunk.toString()
      fullResponse += text
      res.write(`data: ${JSON.stringify({ text })}\n\n`)
    })

    stdout.on('error', (streamErr) => {
      if (turnFinalized)
        return
      turnFinalized = true
      insertTurn({ taskId: task.id, role: 'assistant', content: fullResponse || '[stream error]' })
      const stderrSnippet = getStderr()
      res.write(`event: error\ndata: ${JSON.stringify({ error: String(streamErr), stderr: stderrSnippet || undefined })}\n\n`)
      res.end()
    })

    try {
      await waitForExit()

      if (turnFinalized)
        return
      turnFinalized = true

      const phaseMatch = fullResponse.match(PHASE_DONE_RE)
      const detectedPhase = phaseMatch ? phaseMatch[1] : undefined

      // Update task title from refinedTitle whenever the agent emits it.
      // Use the LAST json block in case earlier turns also contained examples.
      const jsonMatches = [...fullResponse.matchAll(JSON_BLOCK_RE)]
      const jsonMatch = jsonMatches.at(-1)
      if (jsonMatch) {
        try {
          const parsed = JSON.parse(jsonrepair(jsonMatch[1])) as Record<string, unknown>
          if (typeof parsed.refinedTitle === 'string' && parsed.refinedTitle.trim()) {
            updateTask(task.id, { title: parsed.refinedTitle.trim() })
            broadcastEnrichedUpdate(task.id)
          }
        }
        catch {}
      }

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
      res.end()
    }
    catch (err) {
      if (turnFinalized)
        return
      turnFinalized = true
      insertTurn({ taskId: task.id, role: 'assistant', content: fullResponse || '[error]' })
      const stderrSnippet = getStderr()
      res.write(`event: error\ndata: ${JSON.stringify({ error: err instanceof Error ? err.message : 'spawn failed', stderr: stderrSnippet || undefined })}\n\n`)
      res.end()
    }
    finally {
      clearTimeout(timeoutHandle)
      res.removeListener('close', onClose)
      activeTurns.delete(task.id)
      for (const f of tempFiles) {
        try { unlinkSync(f) } catch {}
      }
    }
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

    const jsonMatches = [...lastAssistant.content.matchAll(JSON_BLOCK_RE)]
    const jsonMatch = jsonMatches.at(-1)
    if (!jsonMatch) {
      res.status(409).json({ error: 'No JSON block found in last assistant message' })
      return
    }

    let konzeptOutput: Record<string, unknown>
    try {
      konzeptOutput = JSON.parse(jsonrepair(jsonMatch[1]))
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
    const userId = task.userId ?? null
    const cwd = typeof konzeptOutput.cwd === 'string' ? konzeptOutput.cwd : task.cwd
    applyPresetPermissions(task.id, userId, cwd)
    saveGrantsToPresets(task.id, userId, cwd)
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
    res.json({
      turns: listTurns(req.params.taskId),
      isProcessing: activeTurns.has(req.params.taskId),
    })
  })

  // Mount mutation sub-router so its rejectCrossOrigin middleware guards
  // every POST route registered above.
  router.use(mutationRouter)

  return router
}
