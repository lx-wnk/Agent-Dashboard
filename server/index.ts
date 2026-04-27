import type { TaskEvent } from './routes/taskRoutes.js'
import { Buffer } from 'node:buffer'
import { timingSafeEqual } from 'node:crypto'
import { fstatSync, mkdirSync, openSync } from 'node:fs'
import { readFile } from 'node:fs/promises'
import { createServer as createHttpServer } from 'node:http'
import { homedir } from 'node:os'
import { join } from 'node:path'
import process from 'node:process'
import { consola } from 'consola'
import cookieParser from 'cookie-parser'
import express from 'express'
import { getAgents } from './agentMerger.js'
import { exchangeCodeForToken, getGitHubUser, isOrgMember } from './auth/githubOAuth.js'
import { signJwt } from './auth/jwtUtils.js'
import { isAuthEnabled, requireAuth } from './auth/requireAuth.js'
import { getChannelMap } from './channelDiscovery.js'
import { getDb } from './db/client.js'
import { getTaskById } from './db/tasksRepo.js'
import { upsertUser } from './db/usersRepo.js'
import { parseFullSession } from './jsonlParser.js'
import { createMcpRouter } from './mcp/mcpRouter.js'
import { createDispatcher, setSseBroadcaster } from './notifications/dispatcher.js'
import { DISCOVERY_DIR } from './paths.js'
import { PipelineOrchestrator } from './pipeline/orchestrator.js'
import { aggregateAgents, getRemoteUrls, isRemoteFetch } from './remoteAggregator.js'
import { createApiKeyRouter } from './routes/apiKeyRoutes.js'
import { createTaskRouter, enrichTask } from './routes/taskRoutes.js'
import { getSessions } from './sessionScanner.js'
import { SpawnManager } from './spawnManager.js'
import { getSystemInfo } from './systemMonitor.js'

// Ensure FDs 0–2 are open. When spawned by tsx watch or similar tools, stdio FDs
// can be closed. If they are, the OS reuses those low numbers for new pipes, which
// causes posix_spawn_file_actions_adddup2 to see an unexpected source FD → EBADF.
for (let fd = 0; fd <= 2; fd++) {
  try {
    fstatSync(fd)
  }
  catch {
    openSync('/dev/null', fd === 0 ? 'r' : 'w')
  }
}

// SECURITY: This server exposes session data (prompts, tool outputs, file paths).
// Always bind to 127.0.0.1 — never expose to the network.
const PORT = Number.parseInt(process.env.DASHBOARD_PORT || '13120', 10)
const HOST = process.env.DASHBOARD_HOST ?? '127.0.0.1'
if (HOST !== '127.0.0.1' && HOST !== 'localhost') {
  console.warn(
    `[security] Dashboard bound to ${HOST} — ensure this host is on a trusted network or VPN. Never expose to the public internet.`,
  )
}
const SSE_INTERVAL_MS = (() => {
  const val = Number(process.env.DASHBOARD_SSE_INTERVAL_MS ?? 3000)
  if (!Number.isFinite(val) || val <= 0) {
    console.warn(`[config] DASHBOARD_SSE_INTERVAL_MS invalid (got: ${process.env.DASHBOARD_SSE_INTERVAL_MS}); using 3000ms default`)
    return 3000
  }
  return val
})()
const UUID_RE = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/i

// Spawn state + logic (rate limit, stderr ring-buffer, reply store,
// channel message forwarding) lives in SpawnManager.
const spawnManager = new SpawnManager()

async function start() {
  // Ensure discovery directory exists
  mkdirSync(DISCOVERY_DIR, { recursive: true })

  const app = express()
  app.use(express.json())
  app.use(cookieParser())

  // ─── Auth routes (public — before requireAuth) ───────────

  app.get('/auth/login', (_req, res) => {
    if (!isAuthEnabled()) {
      res.redirect('/')
      return
    }
    const params = new URLSearchParams({
      client_id: process.env.GITHUB_CLIENT_ID!,
      scope: 'read:org',
      redirect_uri: `http://${HOST}:${PORT}/auth/callback`,
    })
    res.redirect(`https://github.com/login/oauth/authorize?${params}`)
  })

  app.get('/auth/callback', async (req, res) => {
    const code = req.query.code as string | undefined
    if (!code) {
      res.status(400).send('Missing code')
      return
    }
    try {
      const accessToken = await exchangeCodeForToken(code)
      const ghUser = await getGitHubUser(accessToken)
      const member = await isOrgMember(ghUser.login, accessToken)
      if (!member) {
        res.status(403).send('You must be a member of the required GitHub org to access this dashboard.')
        return
      }
      const user = upsertUser({ id: ghUser.id, githubLogin: ghUser.login, displayName: ghUser.name, avatarUrl: ghUser.avatar_url })
      const token = signJwt(
        { sub: user.id, login: user.githubLogin, isAdmin: user.isAdmin },
        process.env.JWT_SECRET ?? 'change-me',
        8 * 3600,
      )
      res.cookie('dashboard_session', token, { httpOnly: true, sameSite: 'lax', maxAge: 8 * 3600 * 1000 })
      res.redirect('/')
    }
    catch (err) {
      console.error('[auth] OAuth callback error:', err)
      res.status(500).send('Authentication failed')
    }
  })

  app.post('/auth/logout', (_req, res) => {
    res.clearCookie('dashboard_session')
    res.redirect('/auth/login')
  })

  app.get('/api/me', requireAuth, (req, res) => {
    if (!isAuthEnabled()) {
      res.json({ user: null, isAdmin: true, authEnabled: false })
      return
    }
    res.json({ user: req.user, isAdmin: req.user?.isAdmin ?? false, authEnabled: true })
  })

  // All /api/* routes (except /auth/* and /api/me above) require authentication
  app.use('/api', requireAuth)

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

  // Track the last persisted timestamp to only insert new points on each interval
  let persistedUpTo = 0

  // Load persisted trend on startup (best-effort)
  const costTrend: Array<{ t: number, cost: number, tokens: number }> = (() => {
    try {
      const db = getDb()
      const rows = db.prepare(
        'SELECT t, cost, tokens FROM agent_cost_trend ORDER BY t ASC',
      ).all() as Array<{ t: number, cost: number, tokens: number }>
      if (rows.length > 0)
        persistedUpTo = rows[rows.length - 1].t
      return rows.slice(-MAX_TREND_POINTS)
    }
    catch {
      return []
    }
  })()

  // Persist new trend points every 60s (best-effort — never crash the server)
  const persistTrendId = setInterval(() => {
    const newPoints = costTrend.filter(p => p.t > persistedUpTo)
    if (newPoints.length === 0)
      return
    try {
      const db = getDb()
      const insert = db.prepare('INSERT OR IGNORE INTO agent_cost_trend (t, cost, tokens) VALUES (?, ?, ?)')
      db.transaction(() => {
        for (const p of newPoints)
          insert.run(p.t, p.cost, p.tokens)
        db.prepare(
          'DELETE FROM agent_cost_trend WHERE t NOT IN (SELECT t FROM agent_cost_trend ORDER BY t DESC LIMIT ?)',
        ).run(MAX_TREND_POINTS)
      })()
      persistedUpTo = newPoints[newPoints.length - 1].t
    }
    catch (err) {
      console.warn('[cost-trend] persist failed:', err)
    }
  }, 60_000)
  persistTrendId.unref()

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
    }, SSE_INTERVAL_MS)
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
      // If the task was deleted between the orchestrator firing and this
      // callback running, skip the notification — matches the REST path
      // which also early-returns on !task.
      if (!task)
        return
      dispatcher
        .dispatch({
          eventType: 'on_hold',
          title: `Task "${task.title}" needs permission`,
          body: `Agent requests ${request.tool}${request.pattern ? ` (${request.pattern})` : ''}${request.reason ? `\nReason: ${request.reason}` : ''}`,
          taskId,
          taskSlug: task.slug,
          severity: 'warning',
        })
        .catch(err => consola.warn('[notifications] dispatch failed:', (err as Error).message))
    },
    onTaskChanged: (taskId, info) => {
      // Fired by the orchestrator after every successful applyTransition.
      // Push the latest enriched task row so kanban clients see stage
      // advances, iterations, on_hold/awaiting_user, and done — not just
      // failures. enrichTask adds latestStageRunStatus + needsUser, which
      // the kanban cards bind to for their status chip.
      const task = getTaskById(taskId)
      if (task)
        broadcastTaskEvent({ type: 'task_updated', taskId, payload: enrichTask(task) })
      else
        broadcastTaskEvent({ type: 'stage_run_updated', taskId, payload: info })
    },
    onStageFailed: (taskId, info) => {
      // Push a task_updated event so the SSE clients re-fetch and see
      // the new needsUser flag (derived from latestStageRunStatus='failed').
      broadcastTaskEvent({ type: 'stage_run_updated', taskId, payload: info })
      const task = getTaskById(taskId)
      if (!task)
        return
      dispatcher
        .dispatch({
          eventType: 'failed',
          title: `Task "${task.title}" stage failed`,
          body: `Stage ${info.stage} (iter ${info.iteration}) failed:\n${info.error}`,
          taskId,
          taskSlug: task.slug,
          severity: 'error',
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
        return (url.hostname === HOST || url.hostname === 'localhost' || url.hostname === '127.0.0.1') && url.port === String(PORT)
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

  // API key management routes (browser-facing, CSRF-guarded, no bearer token required)
  app.use('/api', createApiKeyRouter({ rejectCrossOrigin }))

  // Task pipeline routes (must come after rejectCrossOrigin definition)
  app.use('/api', createTaskRouter({
    rejectCrossOrigin,
    orchestrator,
    broadcastTaskEvent,
    dispatcher,
  }))

  // MCP endpoint — stateless, bearer-token authenticated, mounted before
  // the Vite catch-all so POST /api/mcp is never swallowed by the SPA.
  app.use('/api', createMcpRouter(
    orchestrator,
    (taskId) => {
      const task = getTaskById(taskId)
      if (task)
        broadcastTaskEvent({ type: 'task_updated', taskId, payload: enrichTask(task) })
    },
    (taskId) => {
      broadcastTaskEvent({ type: 'task_deleted', taskId })
    },
  ))

  // Spawn a new Claude agent process
  app.post('/api/agents/spawn', (req, res) => {
    if (rejectCrossOrigin(req, res))
      return

    if (!spawnManager.isSpawnAllowed()) {
      const windowSecs = Math.round(spawnManager.getRateLimitConfig().windowMs / 1000)
      const { max } = spawnManager.getRateLimitConfig()
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
  app.get('/api/agents/spawn/:pid/status', (req, res) => {
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

      const replies = spawnManager.getReplies(agent.pid, since)

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

  // Global Express error middleware — must come after all routes/middleware.
  // Express detects error handlers by their 4-parameter signature.
  app.use((err: unknown, _req: express.Request, res: express.Response, _next: express.NextFunction) => {
    const message = err instanceof Error ? err.message : 'Internal server error'
    consola.error('Unhandled route error', err)
    if (!res.headersSent)
      res.status(500).json({ error: message })
  })

  httpServer.listen(PORT, HOST, () => {
    const mode = isProd ? 'production' : 'development'
    consola.info(`Claude Agent Overview (${mode}) running at http://localhost:${PORT}`)
  })
}

start()
