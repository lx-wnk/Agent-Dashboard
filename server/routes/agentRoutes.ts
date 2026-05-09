import type { Router as ExpressRouter, Request, RequestHandler, Response } from 'express'
import type { SpawnManager } from '../spawnManager.js'
import { Buffer } from 'node:buffer'
import { timingSafeEqual } from 'node:crypto'
import { readFile } from 'node:fs/promises'
import { join } from 'node:path'
import { consola } from 'consola'
import { Router } from 'express'
import { getAgents } from '../agentMerger.js'
import { isAuthEnabled } from '../auth/requireAuth.js'
import { getChannelMap } from '../channelDiscovery.js'
import { UUID_RE } from '../constants.js'
import { findStageRunBySessionId } from '../db/stageRunsRepo.js'
import { getTaskById } from '../db/tasksRepo.js'
import { parseFullSession } from '../jsonlParser.js'
import { DISCOVERY_DIR } from '../paths.js'
import { aggregateAgents, getEnvRemoteTargets, isRemoteFetch } from '../remoteAggregator.js'
import { getSessions } from '../sessionScanner.js'
import { canAccessTask } from './taskRoutes.js'

interface AgentRouterDeps {
  spawnManager: SpawnManager
  requireApiToken: RequestHandler
  rejectCrossOrigin: (req: Request, res: Response) => boolean
}

export function createAgentRouter({ spawnManager, requireApiToken, rejectCrossOrigin }: AgentRouterDeps): ExpressRouter {
  const router = Router()

  router.get('/agents', requireApiToken, async (req, res) => {
    try {
      const localAgents = await getAgents()
      // If this request comes from another dashboard (X-Dashboard-Origin), return local-only to prevent chains
      if (isRemoteFetch(req.headers)) {
        res.json(localAgents)
        return
      }
      const remotes = getEnvRemoteTargets()
      const agents = remotes.length > 0 ? await aggregateAgents(localAgents, remotes) : localAgents
      res.json(agents)
    }
    catch (err) {
      console.error('Error fetching agents:', err)
      res.status(500).json({ error: 'Failed to fetch agents' })
    }
  })

  router.get('/sessions', async (_req, res) => {
    try {
      const sessions = await getSessions()
      res.json(sessions)
    }
    catch (err) {
      console.error('Error fetching sessions:', err)
      res.status(500).json({ error: 'Failed to fetch sessions' })
    }
  })

  // Spawn a new Claude agent process
  router.post('/agents/spawn', (req, res) => {
    if (rejectCrossOrigin(req, res))
      return

    if (!req.user?.isAdmin) {
      res.status(403).json({ error: 'Admin access required to spawn agents' })
      return
    }

    if (!spawnManager.isSpawnAllowed()) {
      const { windowMs, max } = spawnManager.getRateLimitConfig()
      const windowSecs = Math.round(windowMs / 1000)
      const ip = req.ip ?? req.socket.remoteAddress ?? 'unknown'
      consola.warn(`[spawnManager] rate limit hit from ${ip} (max ${max}/${windowSecs}s)`)
      res.status(429).json({ error: `Too many spawn requests. Max ${max} per ${windowSecs} seconds.` })
      return
    }

    const result = spawnManager.spawnAgent(req.body)
    if (!result.ok) {
      res.status(result.status).json({ error: result.error })
      return
    }
    res.json({ ok: true, pid: result.pid })
  })

  // Get status of a spawned agent
  router.get('/agents/spawn/:pid/status', (req, res) => {
    const pid = Number.parseInt(req.params.pid, 10)
    if (Number.isNaN(pid) || String(pid) !== req.params.pid) {
      res.status(400).json({ error: 'Invalid PID' })
      return
    }
    const status = spawnManager.getStatus(pid)
    if (!status) {
      res.status(404).json({ error: 'Unknown spawn PID' })
      return
    }
    res.json(status)
  })

  router.get('/agents/:sessionId/output', async (req, res) => {
    try {
      const { sessionId } = req.params
      if (!UUID_RE.test(sessionId)) {
        res.status(400).json({ error: 'Invalid sessionId format' })
        return
      }

      // In multi-user mode, verify the requesting user can access this session.
      // Pipeline sessions are linked to tasks via stage_runs; check task ownership.
      // Non-pipeline sessions (manual spawns) are admin-only in multi-user mode.
      if (isAuthEnabled() && req.user && !req.user.isAdmin) {
        const stageRun = findStageRunBySessionId(sessionId)
        if (stageRun) {
          const task = getTaskById(stageRun.taskId)
          if (!task || !canAccessTask(task, req.user)) {
            res.status(404).json({ error: 'Session not found' })
            return
          }
        }
        else {
          // Session not linked to any pipeline task — only admins can read these
          // (they may be manual spawns that belong to another user's agent)
          res.status(404).json({ error: 'Session not found' })
          return
        }
      }

      const lastOnly = req.query.last === '1'
      const messages = await parseFullSession(sessionId, lastOnly)
      res.json({ messages })
    }
    catch {
      res.status(500).json({ error: 'Failed to read session output' })
    }
  })

  // Send a message to a running agent via its channel
  router.post('/agents/:sessionId/message', async (req, res) => {
    if (rejectCrossOrigin(req, res))
      return
    try {
      const { sessionId } = req.params
      if (!UUID_RE.test(sessionId)) {
        res.status(400).json({ error: 'Invalid sessionId format' })
        return
      }
      const { message } = req.body

      if (!message || typeof message !== 'string') {
        res.status(400).json({ error: 'Missing "message" field' })
        return
      }

      const agents = await getAgents()
      const agent = agents.find(a => a.sessionId === sessionId)
      if (!agent) {
        res.status(404).json({ error: 'Agent not found' })
        return
      }
      if (!agent.channelAvailable) {
        res.status(404).json({ error: 'Channel not available' })
        return
      }

      const channelMap = await getChannelMap()
      const result = await spawnManager.sendMessageToChannel(agent, message, channelMap)

      switch (result.kind) {
        case 'not_found':
          res.status(404).json({ error: 'Channel not available' })
          return
        case 'timeout':
          res.status(504).json({ error: 'Channel request timed out' })
          return
        case 'unreachable':
          res.status(502).json({ error: `Channel unreachable: ${result.message}` })
          return
        case 'response':
          res.status(result.status).json(result.body)
      }
    }
    catch (err) {
      console.error('Error sending message:', err)
      res.status(500).json({ error: 'Internal error' })
    }
  })

  // Receive a reply FROM a channel MCP server (called by dashboard_reply tool)
  router.post('/channel-reply', async (req, res) => {
    try {
      const { parentPid, message, timestamp } = req.body

      if (!parentPid || !message || !timestamp) {
        res.status(400).json({ error: 'Missing required fields' })
        return
      }

      // Validate token against discovery file
      const authHeader = req.headers.authorization
      if (!authHeader?.startsWith('Bearer ')) {
        res.status(401).json({ error: 'Missing authorization' })
        return
      }
      const token = authHeader.slice(7)

      try {
        const discoveryPath = join(DISCOVERY_DIR, `${parentPid}.json`)
        const raw = await readFile(discoveryPath, 'utf-8')
        const discovery = JSON.parse(raw)
        const expected = Buffer.from(String(discovery.token))
        const provided = Buffer.from(token)
        if (expected.length !== provided.length || !timingSafeEqual(expected, provided)) {
          res.status(403).json({ error: 'Invalid token' })
          return
        }
      }
      catch {
        res.status(403).json({ error: 'Invalid token' })
        return
      }

      spawnManager.storeReply(parentPid, message, timestamp)
      res.json({ ok: true })
    }
    catch (err) {
      console.error('Error handling channel reply:', err)
      res.status(500).json({ error: 'Internal error' })
    }
  })

  router.get('/sessions/:sessionId/timeline', async (req, res) => {
    const { sessionId } = req.params
    if (!UUID_RE.test(sessionId)) {
      res.status(400).json({ error: 'Invalid sessionId format' })
      return
    }
    try {
      const messages = await parseFullSession(sessionId, false)
      const toolCalls = messages.filter(m => m.role === 'tool_call' && m.timestamp)
      res.json({ toolCalls })
    }
    catch {
      res.status(500).json({ error: 'Failed to read session timeline' })
    }
  })

  // Get replies from a specific agent
  router.get('/agents/:sessionId/replies', async (req, res) => {
    try {
      const { sessionId } = req.params
      if (!UUID_RE.test(sessionId)) {
        res.status(400).json({ error: 'Invalid sessionId format' })
        return
      }
      const since = req.query.since as string | undefined

      const agents = await getAgents()
      const agent = agents.find(a => a.sessionId === sessionId)
      if (!agent) {
        res.status(404).json({ error: 'Agent not found' })
        return
      }

      const replies = spawnManager.getReplies(agent.pid, since)

      res.json({ replies })
    }
    catch (err) {
      console.error('Error fetching replies:', err)
      res.status(500).json({ error: 'Internal error' })
    }
  })

  return router
}
