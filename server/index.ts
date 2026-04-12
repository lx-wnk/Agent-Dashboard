import type { Buffer } from 'node:buffer'
import type { TaskEvent } from './routes/taskRoutes.js'
import { spawn } from 'node:child_process'
import { existsSync, mkdirSync, realpathSync } from 'node:fs'
import { readFile } from 'node:fs/promises'
import { createServer as createHttpServer } from 'node:http'
import { homedir } from 'node:os'
import { join } from 'node:path'
import process from 'node:process'
import { consola } from 'consola'
import express from 'express'
import { getAgents } from './agentMerger.js'
import { getChannelMap } from './channelDiscovery.js'
import { getTaskById } from './db/tasksRepo.js'
import { parseFullSession } from './jsonlParser.js'
import { createDispatcher, setSseBroadcaster } from './notifications/dispatcher.js'
import { DISCOVERY_DIR } from './paths.js'
import { PipelineOrchestrator } from './pipeline/orchestrator.js'
import { aggregateAgents, getRemoteUrls, isRemoteFetch } from './remoteAggregator.js'
import { createTaskRouter } from './routes/taskRoutes.js'
import { getSessions } from './sessionScanner.js'
import { getSystemInfo } from './systemMonitor.js'

// SECURITY: This server exposes session data (prompts, tool outputs, file paths).
// Always bind to 127.0.0.1 — never expose to the network.
const PORT = Number.parseInt(process.env.DASHBOARD_PORT || '13120', 10)
const ALLOWED_MODELS = new Set(['claude-opus-4-6', 'claude-sonnet-4-6', 'claude-haiku-4-5', '']) // empty string = "Auto" (no --model flag)
const UUID_RE = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/i
const SPAWN_STORE_MAX_AGE_MS = 60 * 60 * 1000 // 1 hour
const MAX_REPLIES_PER_PID = 50
const MAX_STDERR_BYTES = 4096
const CHANNEL_DIR = join(import.meta.dirname, '..', 'channel')
const CHANNEL_SCRIPT = join(CHANNEL_DIR, 'dashboard-channel.ts')
const CHANNEL_TSX = join(CHANNEL_DIR, 'node_modules', '.bin', 'tsx')

// In-memory spawn status: pid → { status, exitCode, stderr }
interface SpawnStatus {
  pid: number
  status: 'running' | 'exited' | 'error'
  exitCode: number | null
  stderr: string
  startedAt: string
  prompt: string
  cwd: string
}
const spawnStore = new Map<number, SpawnStatus>()

// Rate limiting for spawn endpoint: timestamps of recent requests
const spawnTimestamps: number[] = []

// In-memory reply store: parentPid → replies (ring buffer)
const replyStore = new Map<number, Array<{ message: string, timestamp: string }>>()

function storeReply(pid: number, message: string, timestamp: string) {
  let replies = replyStore.get(pid)
  if (!replies) {
    replies = []
    replyStore.set(pid, replies)
  }
  replies.push({ message, timestamp })
  if (replies.length > MAX_REPLIES_PER_PID) {
    replies.shift()
  }
}

async function start() {
  // Ensure discovery directory exists
  mkdirSync(DISCOVERY_DIR, { recursive: true })

  const app = express()
  app.use(express.json())

  // ─── API routes (before Vite middleware) ────────────────

  // Dashboard config (script path, home dir)
  app.get('/api/config', (_req, res) => {
    const home = homedir()
    const scriptAbsolute = join(import.meta.dirname, '..', 'scripts', 'claude-with-channel.sh')
    const scriptPath = scriptAbsolute.startsWith(home)
      ? `~${scriptAbsolute.slice(home.length)}`
      : scriptAbsolute
    res.json({ scriptPath, homedir: home })
  })

  app.get('/api/agents', async (req, res) => {
    try {
      const localAgents = await getAgents()
      // If this request comes from another dashboard (X-Dashboard-Origin), return local-only to prevent chains
      if (isRemoteFetch(req.headers)) {
        res.json(localAgents)
        return
      }
      const remoteUrls = getRemoteUrls()
      const agents = remoteUrls.length > 0 ? await aggregateAgents(localAgents, remoteUrls) : localAgents
      res.json(agents)
    }
    catch (err) {
      console.error('Error fetching agents:', err)
      res.status(500).json({ error: 'Failed to fetch agents' })
    }
  })

  // SSE stream for real-time agent updates (replaces client-side polling)
  const sseClients = new Set<express.Response>()

  app.get('/api/agents/stream', (_req, res) => {
    res.writeHead(200, {
      'Content-Type': 'text/event-stream',
      'Cache-Control': 'no-cache',
      'Connection': 'keep-alive',
      'X-Accel-Buffering': 'no', // disable nginx buffering if proxied
    })
    res.flushHeaders()

    sseClients.add(res)
    startSSEBroadcast()
    _req.on('close', () => {
      sseClients.delete(res)
      stopSSEBroadcast()
    })
  })

  // Cost trend history: ring buffer, 1h at 3s interval = 1200 entries
  const MAX_TREND_POINTS = 1200
  const costTrend: Array<{ t: number, cost: number, tokens: number }> = []

  // SSE broadcast + cost trend recording: only scan processes when clients are connected
  let sseBroadcastId: ReturnType<typeof setInterval> | null = null

  function startSSEBroadcast() {
    if (sseBroadcastId)
      return
    sseBroadcastId = setInterval(async () => {
      try {
        const localAgents = await getAgents()
        const remoteUrls = getRemoteUrls()
        const agents = remoteUrls.length > 0 ? await aggregateAgents(localAgents, remoteUrls) : localAgents

        // Record cost trend point
        const totalCost = agents.reduce((sum, a) => sum + a.costEstimate, 0)
        const totalTokens = agents.reduce((sum, a) => {
          const u = a.tokenUsage
          return sum + u.inputTokens + u.outputTokens + u.cacheReadTokens + u.cacheCreationTokens
        }, 0)
        costTrend.push({ t: Date.now(), cost: totalCost, tokens: totalTokens })
        if (costTrend.length > MAX_TREND_POINTS) {
          costTrend.shift()
        }

        // Send agents + trend data in a single SSE event
        const payload = JSON.stringify({ agents, trend: costTrend.slice(-60) })
        const data = `data: ${payload}\n\n`
        for (const client of sseClients) {
          try {
            if (!client.writableEnded)
              client.write(data)
          }
          catch {
            sseClients.delete(client)
          }
        }
      }
      catch (err) {
        console.error('SSE broadcast error:', err)
      }
    }, 3000)
  }

  function stopSSEBroadcast() {
    if (sseBroadcastId && sseClients.size === 0) {
      clearInterval(sseBroadcastId)
      sseBroadcastId = null
    }
  }

  app.get('/api/sessions', async (_req, res) => {
    try {
      const sessions = await getSessions()
      res.json(sessions)
    }
    catch (err) {
      console.error('Error fetching sessions:', err)
      res.status(500).json({ error: 'Failed to fetch sessions' })
    }
  })

  // ─── Task Pipeline SSE + Router ────────────────

  const taskSseClients = new Set<express.Response>()
  function broadcastTaskEvent(event: TaskEvent) {
    const data = `data: ${JSON.stringify(event)}\n\n`
    for (const client of taskSseClients) {
      try {
        if (!client.writableEnded)
          client.write(data)
      }
      catch {
        taskSseClients.delete(client)
      }
    }
  }

  app.get('/api/tasks/stream', (_req, res) => {
    res.writeHead(200, {
      'Content-Type': 'text/event-stream',
      'Cache-Control': 'no-cache',
      'Connection': 'keep-alive',
      'X-Accel-Buffering': 'no',
    })
    res.flushHeaders()
    taskSseClients.add(res)
    _req.on('close', () => {
      taskSseClients.delete(res)
    })
  })

  // Browser notifications are pushed through the task SSE stream.
  setSseBroadcaster((payload) => {
    broadcastTaskEvent({
      type: 'stage_run_updated', // reuse event channel for notifications
      taskId: payload.taskId,
      payload: { notification: payload },
    })
  })
  const dispatcher = createDispatcher()

  const orchestrator = new PipelineOrchestrator({
    onPermissionRequest: (taskId, request) => {
      broadcastTaskEvent({ type: 'permission_request', taskId, payload: request })
      // Look up the task for a meaningful notification title. The lookup
      // is a single synchronous SQLite read — cheap, and ensures both the
      // REST and orchestrator-driven notification paths produce identical
      // payloads.
      const task = getTaskById(taskId)
      dispatcher
        .dispatch({
          eventType: 'on_hold',
          title: task ? `Task "${task.title}" needs permission` : 'Agent requested permission',
          body: `Agent requests ${request.tool}${request.pattern ? ` (${request.pattern})` : ''}${request.reason ? `\nReason: ${request.reason}` : ''}`,
          taskId,
          taskSlug: task?.slug ?? taskId,
          severity: 'warning',
        })
        .catch(err => consola.warn('[notifications] dispatch failed:', (err as Error).message))
    },
  })
  orchestrator.start()

  // CSRF protection for mutation endpoints
  function rejectCrossOrigin(req: express.Request, res: express.Response): boolean {
    const origin = req.headers.origin || ''
    const referer = req.headers.referer || ''
    // Allow requests with no origin (non-browser clients like curl)
    if (!origin && !referer)
      return false
    const allowed = (s: string) => {
      try {
        const url = new URL(s)
        return (url.hostname === 'localhost' || url.hostname === '127.0.0.1') && url.port === String(PORT)
      }
      catch {
        return false
      }
    }
    if (allowed(origin) || allowed(referer))
      return false
    res.status(403).json({ error: 'Cross-origin request blocked' })
    return true
  }

  // Task pipeline routes (must come after rejectCrossOrigin definition)
  app.use('/api', createTaskRouter({
    rejectCrossOrigin,
    orchestrator,
    broadcastTaskEvent,
    dispatcher,
  }))

  // Spawn a new Claude agent process
  app.post('/api/agents/spawn', (req, res) => {
    if (rejectCrossOrigin(req, res))
      return

    // Rate limit: max 5 spawn requests per minute (sliding window)
    const now = Date.now()
    const windowStart = now - 60_000
    // Prune timestamps older than 60 seconds
    while (spawnTimestamps.length > 0 && spawnTimestamps[0] <= windowStart) {
      spawnTimestamps.shift()
    }
    if (spawnTimestamps.length >= 5) {
      res.status(429).json({ error: 'Too many spawn requests. Max 5 per minute.' })
      return
    }
    spawnTimestamps.push(now)

    try {
      const { prompt, cwd, model, systemPrompt, enableChannel, skipPermissions, resumeSessionId } = req.body

      if (!prompt || typeof prompt !== 'string') {
        res.status(400).json({ error: 'Missing or invalid "prompt" field' })
        return
      }
      if (!cwd || typeof cwd !== 'string') {
        res.status(400).json({ error: 'Missing or invalid "cwd" field' })
        return
      }
      if (!existsSync(cwd)) {
        res.status(400).json({ error: `Directory does not exist: ${cwd}` })
        return
      }
      if (model && !ALLOWED_MODELS.has(model)) {
        res.status(400).json({ error: 'Invalid model' })
        return
      }
      if (resumeSessionId && !UUID_RE.test(resumeSessionId)) {
        res.status(400).json({ error: 'Invalid sessionId format' })
        return
      }

      const args: string[] = []
      if (skipPermissions) {
        args.push('--dangerously-skip-permissions')
      }
      if (resumeSessionId) {
        args.push('--resume', resumeSessionId)
      }
      args.push('-p', prompt)
      if (model) {
        args.push('--model', model)
      }
      if (systemPrompt && typeof systemPrompt === 'string') {
        args.push('--system-prompt', systemPrompt.slice(0, 10000))
      }
      if (enableChannel !== false) {
        const mcpConfig = JSON.stringify({
          mcpServers: {
            'dashboard-channel': {
              command: realpathSync(CHANNEL_TSX),
              args: [realpathSync(CHANNEL_SCRIPT)],
            },
          },
        })
        args.push('--mcp-config', mcpConfig)
      }

      const child = spawn('claude', args, {
        cwd,
        detached: true,
        stdio: ['ignore', 'ignore', 'pipe'], // capture stderr
      })

      const pid = child.pid ?? 0
      const status: SpawnStatus = {
        pid,
        status: 'running',
        exitCode: null,
        stderr: '',
        startedAt: new Date().toISOString(),
        prompt: prompt.slice(0, 200),
        cwd,
      }
      spawnStore.set(pid, status)

      // Collect stderr
      child.stderr!.on('data', (chunk: Buffer) => {
        status.stderr += chunk.toString()
        if (status.stderr.length > MAX_STDERR_BYTES) {
          status.stderr = status.stderr.slice(-MAX_STDERR_BYTES)
        }
      })

      child.on('exit', (code) => {
        status.status = 'exited'
        status.exitCode = code
        consola.info(`[spawn] PID ${pid} exited with code ${code}`)
        if (status.stderr) {
          console.error(`[spawn] PID ${pid} stderr:\n${status.stderr}`)
        }
        // Prune old entries to prevent memory leak
        for (const [key, entry] of spawnStore) {
          if (Date.now() - new Date(entry.startedAt).getTime() > SPAWN_STORE_MAX_AGE_MS) {
            spawnStore.delete(key)
          }
        }
      })

      child.on('error', (err) => {
        status.status = 'error'
        status.stderr += `\nSpawn error: ${err.message}`
        console.error(`[spawn] PID ${pid} error:`, err)
      })

      child.unref()
      res.json({ ok: true, pid })
    }
    catch (err) {
      console.error('Error spawning agent:', err)
      res.status(500).json({ error: 'Internal error' })
    }
  })

  // Get status of a spawned agent
  app.get('/api/agents/spawn/:pid/status', (req, res) => {
    const pid = Number.parseInt(req.params.pid, 10)
    if (Number.isNaN(pid) || String(pid) !== req.params.pid) {
      res.status(400).json({ error: 'Invalid PID' })
      return
    }
    const status = spawnStore.get(pid)
    if (!status) {
      res.status(404).json({ error: 'Unknown spawn PID' })
      return
    }
    res.json(status)
  })

  app.get('/api/agents/:sessionId/output', async (req, res) => {
    try {
      const { sessionId } = req.params
      if (!UUID_RE.test(sessionId)) {
        res.status(400).json({ error: 'Invalid sessionId format' })
        return
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
  app.post('/api/agents/:sessionId/message', async (req, res) => {
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
      let channel = channelMap.get(agent.pid)
      // Fallback: match by cwd if PID-based lookup missed
      if (!channel) {
        for (const [, info] of channelMap) {
          if (info.cwd && info.cwd === agent.cwd) {
            channel = info
            break
          }
        }
      }
      if (!channel) {
        res.status(404).json({ error: 'Channel not available' })
        return
      }

      const controller = new AbortController()
      const timeout = setTimeout(() => controller.abort(), 5000)

      try {
        const response = await fetch(`http://127.0.0.1:${channel.port}/message`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ message }),
          signal: controller.signal,
        })
        clearTimeout(timeout)

        const data = await response.json()
        res.status(response.status).json(data)
      }
      catch (err) {
        clearTimeout(timeout)
        if ((err as Error).name === 'AbortError') {
          res.status(504).json({ error: 'Channel request timed out' })
        }
        else {
          res.status(502).json({ error: `Channel unreachable: ${(err as Error).message}` })
        }
      }
    }
    catch (err) {
      console.error('Error sending message:', err)
      res.status(500).json({ error: 'Internal error' })
    }
  })

  // Receive a reply FROM a channel MCP server (called by dashboard_reply tool)
  app.post('/api/channel-reply', async (req, res) => {
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
        if (discovery.token !== token) {
          res.status(403).json({ error: 'Invalid token' })
          return
        }
      }
      catch {
        res.status(403).json({ error: 'Invalid token' })
        return
      }

      storeReply(parentPid, message, timestamp)
      res.json({ ok: true })
    }
    catch (err) {
      console.error('Error handling channel reply:', err)
      res.status(500).json({ error: 'Internal error' })
    }
  })

  // Get replies from a specific agent
  app.get('/api/agents/:sessionId/replies', async (req, res) => {
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

      let replies = replyStore.get(agent.pid) || []
      if (since) {
        const sinceTime = new Date(since).getTime()
        replies = replies.filter(r => new Date(r.timestamp).getTime() > sinceTime)
      }

      res.json({ replies })
    }
    catch (err) {
      console.error('Error fetching replies:', err)
      res.status(500).json({ error: 'Internal error' })
    }
  })

  app.get('/api/system', async (_req, res) => {
    try {
      const info = await getSystemInfo()
      res.json(info)
    }
    catch (err) {
      console.error('Error fetching system info:', err)
      res.status(500).json({ error: 'Failed to fetch system info' })
    }
  })

  // ─── HTTP server ───────────────────────────────────────

  const httpServer = createHttpServer(app)
  const isProd = process.env.NODE_ENV === 'production'

  if (isProd) {
    const distPath = join(import.meta.dirname, '..', 'dist')
    app.use(express.static(distPath))
    // SPA fallback: serve index.html for all non-API routes
    app.get('*', (_req, res) => {
      res.sendFile(join(distPath, 'index.html'))
    })
  }
  else {
    const { createServer: createViteServer } = await import('vite')
    const vite = await createViteServer({
      server: { middlewareMode: true, hmr: { server: httpServer } },
      appType: 'spa',
    })
    app.use(vite.middlewares)
  }

  httpServer.listen(PORT, '127.0.0.1', () => {
    const mode = isProd ? 'production' : 'development'
    consola.info(`Claude Agent Overview (${mode}) running at http://localhost:${PORT}`)
  })
}

start()
