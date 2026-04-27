import type { TaskEvent } from './routes/taskRoutes.js'
import { fstatSync, mkdirSync, openSync } from 'node:fs'
import { createServer as createHttpServer } from 'node:http'
import { homedir } from 'node:os'
import { join } from 'node:path'
import process from 'node:process'
import { consola } from 'consola'
import cookieParser from 'cookie-parser'
import express from 'express'
import { getAgents } from './agentMerger.js'
import { isAuthEnabled, requireAuth } from './auth/requireAuth.js'
import { getDb } from './db/client.js'
import { listRemoteRegistrationsForUser } from './db/remoteRegistrationsRepo.js'
import { getTaskById } from './db/tasksRepo.js'
import { createMcpRouter } from './mcp/mcpRouter.js'
import { createRejectCrossOrigin, requireApiToken } from './middleware.js'
import { createDispatcher, setSseBroadcaster } from './notifications/dispatcher.js'
import { DISCOVERY_DIR } from './paths.js'
import { PipelineOrchestrator } from './pipeline/orchestrator.js'
import { aggregateAgents, getEnvRemoteTargets } from './remoteAggregator.js'
import { createAgentRouter } from './routes/agentRoutes.js'
import { createApiKeyRouter } from './routes/apiKeyRoutes.js'
import { createAuthRouter } from './routes/authRoutes.js'
import { createRemoteRouter } from './routes/remoteRoutes.js'
import { createTaskRouter, enrichTask } from './routes/taskRoutes.js'
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

  app.use(createAuthRouter({ host: HOST, port: PORT }))

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

  // SSE stream for real-time agent updates (replaces client-side polling)
  interface SseClient { res: express.Response, userId: string, isAdmin: boolean }
  const sseClients = new Set<SseClient>()

  app.get('/api/agents/stream', requireApiToken, (req, res) => {
    res.writeHead(200, {
      'Content-Type': 'text/event-stream',
      'Cache-Control': 'no-cache',
      'Connection': 'keep-alive',
      'X-Accel-Buffering': 'no', // disable nginx buffering if proxied
    })
    res.flushHeaders()

    const client: SseClient = { res, userId: req.user!.id, isAdmin: req.user!.isAdmin }
    sseClients.add(client)
    startSSEBroadcast()
    req.on('close', () => {
      sseClients.delete(client)
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
        const envRemotes = getEnvRemoteTargets()

        // Baseline aggregation for cost trend (env remotes only — shared across all users)
        const baselineAgents = await aggregateAgents(localAgents, envRemotes)
        const totalCost = baselineAgents.reduce((sum, a) => sum + a.costEstimate, 0)
        const totalTokens = baselineAgents.reduce((sum, a) => {
          const u = a.tokenUsage
          return sum + u.inputTokens + u.outputTokens + u.cacheReadTokens + u.cacheCreationTokens
        }, 0)
        costTrend.push({ t: Date.now(), cost: totalCost, tokens: totalTokens })
        if (costTrend.length > MAX_TREND_POINTS)
          costTrend.shift()

        const trendSlice = costTrend.slice(-60)

        // Fan out: each client gets local agents + their own remotes
        await Promise.all([...sseClients].map(async (client) => {
          try {
            if (client.res.writableEnded)
              return

            const userRemotes = isAuthEnabled()
              ? listRemoteRegistrationsForUser(client.userId).map(r => ({
                  url: r.url,
                  bearerKey: r.bearerKey,
                  name: r.name,
                }))
              : []

            const allRemotes = [...envRemotes, ...userRemotes]
            const agents = await aggregateAgents(localAgents, allRemotes)
            const payload = JSON.stringify({ agents, trend: trendSlice })
            client.res.write(`data: ${payload}\n\n`)
          }
          catch {
            sseClients.delete(client)
          }
        }))
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

  const rejectCrossOrigin = createRejectCrossOrigin(HOST, PORT)

  // API key management routes (browser-facing, CSRF-guarded, no bearer token required)
  app.use('/api', createApiKeyRouter({ rejectCrossOrigin }))

  // Task pipeline routes (must come after rejectCrossOrigin definition)
  app.use('/api', createTaskRouter({
    rejectCrossOrigin,
    orchestrator,
    broadcastTaskEvent,
    dispatcher,
  }))

  // Remote dashboard registration routes (per-user CRUD)
  app.use('/api/remotes', createRemoteRouter())

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

  // Agent routes (REST endpoints — non-SSE; SSE stream stays above)
  app.use('/api', createAgentRouter({ spawnManager, requireApiToken, rejectCrossOrigin }))

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
