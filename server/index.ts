import type { Agent } from '../src/types.js'
import type { TaskEvent } from './routes/taskRoutes.js'
import { fstatSync, mkdirSync, openSync } from 'node:fs'
import { createServer as createHttpServer } from 'node:http'
import { join } from 'node:path'
import process from 'node:process'
import { consola } from 'consola'
import cookieParser from 'cookie-parser'
import express from 'express'
import { getAgents } from './agentMerger.js'
import { discoverPatterns } from './analytics/ngrams.js'
import { isAuthEnabled, requireAuth } from './auth/requireAuth.js'
import { DEFAULT_DASHBOARD_PORT, LOOPBACK_HOST, resolveDashboardPort } from './constants.js'
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
import { createHistoryRouter } from './routes/historyRoutes.js'
import { createHooksRouter } from './routes/hooksRoutes.js'
import { createMemoryRouter } from './routes/memoryRoutes.js'
import { createPresetRouter } from './routes/presetRoutes.js'
import { createRefineRouter } from './routes/refineRoutes.js'
import { createRemoteRouter } from './routes/remoteRoutes.js'
import { createSearchRouter } from './routes/searchRoutes.js'
import { createSystemRouter } from './routes/systemRoutes.js'
import { createTaskRouter, enrichTask } from './routes/taskRoutes.js'
import { createWebPushRouter } from './routes/webpushRoutes.js'
import { SpawnManager } from './spawnManager.js'

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
// Always bind to LOOPBACK_HOST — never expose to the network.
const PORT = (() => {
  const raw = process.env.DASHBOARD_PORT
  if (raw && (!Number.isInteger(Number.parseInt(raw, 10)) || Number.parseInt(raw, 10) < 1 || Number.parseInt(raw, 10) > 65535))
    console.warn(`[config] DASHBOARD_PORT invalid (got: ${raw}); using ${DEFAULT_DASHBOARD_PORT} default`)
  return resolveDashboardPort()
})()
const HOST = process.env.DASHBOARD_HOST ?? LOOPBACK_HOST
if (HOST !== LOOPBACK_HOST && HOST !== 'localhost') {
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

const HOOKS_SECRET = process.env.DASHBOARD_HOOKS_SECRET ?? ''
const HOOKS_DEBOUNCE_MS = (() => {
  const val = Number(process.env.DASHBOARD_HOOKS_DEBOUNCE_MS ?? 100)
  return Number.isFinite(val) && val >= 0 ? val : 100
})()

// Spawn state + logic (rate limit, stderr ring-buffer, reply store,
// channel message forwarding) lives in SpawnManager.
const spawnManager = new SpawnManager()

async function start() {
  // Ensure discovery directory exists
  mkdirSync(DISCOVERY_DIR, { recursive: true })

  const app = express()
  app.use(express.json({ limit: '10mb' }))
  app.use(cookieParser())

  // Security headers — applied to every response before any route handler
  app.use((_req, res, next) => {
    res.setHeader('X-Content-Type-Options', 'nosniff')
    res.setHeader('X-Frame-Options', 'DENY')
    const isDev = process.env.NODE_ENV !== 'production'
    const csp = [
      `default-src 'self'`,
      isDev ? `script-src 'self' 'unsafe-eval'` : `script-src 'self'`,
      `style-src 'self' 'unsafe-inline'`,
      `connect-src 'self' ${isDev ? 'ws: wss:' : ''}`.trim(),
      `img-src 'self' data:`,
      `font-src 'self'`,
      `frame-ancestors 'none'`,
    ].join('; ')
    res.setHeader('Content-Security-Policy', csp)
    next()
  })

  // ─── Auth routes (public — before requireAuth) ───────────

  app.use(createAuthRouter({ host: HOST, port: PORT }))

  // Hooks endpoint is exempt from session auth — protected by shared secret only.
  app.use('/api/hooks', createHooksRouter({ onEvent: scheduleHooksRescan, secret: HOOKS_SECRET }))

  // All /api/* routes (except /auth/* and /api/me above) require authentication
  app.use('/api', requireAuth)

  // ─── API routes (before Vite middleware) ────────────────

  // System routes: /api/config, /api/system
  app.use('/api', createSystemRouter({ serverDir: import.meta.dirname }))

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

  // Cached snapshot of the last local agent scan — used by the search endpoint
  // to avoid re-scanning processes on every search request.
  let cachedAgents: Agent[] = []

  // SSE broadcast + cost trend recording: only scan processes when clients are connected
  let sseBroadcastId: ReturnType<typeof setInterval> | null = null
  let hooksDebounceId: ReturnType<typeof setTimeout> | null = null

  async function broadcastAgents(localAgents: Awaited<ReturnType<typeof getAgents>>): Promise<void> {
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

    // Fan out: each client gets local agents + their own remotes.
    // Deduplicate: build per-userId payload cache so multiple browser tabs
    // from the same user don't trigger duplicate remote-agent fetches.
    const userPayloadCache = new Map<string, string>()

    await Promise.all([...sseClients].map(async (client) => {
      try {
        if (client.res.writableEnded)
          return

        if (!userPayloadCache.has(client.userId)) {
          const userRemotes = isAuthEnabled()
            ? listRemoteRegistrationsForUser(client.userId).map(r => ({
                url: r.url,
                bearerKey: r.bearerKey,
                name: r.name,
              }))
            : []
          const allRemotes = [...envRemotes, ...userRemotes]
          const agents = await aggregateAgents(localAgents, allRemotes)
          userPayloadCache.set(client.userId, JSON.stringify({ agents, trend: trendSlice }))
        }

        const payload = userPayloadCache.get(client.userId)!
        client.res.write(`data: ${payload}\n\n`)
      }
      catch {
        sseClients.delete(client)
      }
    }))
  }

  function scheduleHooksRescan(): void {
    if (hooksDebounceId)
      clearTimeout(hooksDebounceId)
    hooksDebounceId = setTimeout(async () => {
      hooksDebounceId = null
      if (sseClients.size === 0)
        return
      try {
        const localAgents = await getAgents()
        await broadcastAgents(localAgents)
      }
      catch (err) {
        console.warn('[hooks] rescan failed:', err)
      }
    }, HOOKS_DEBOUNCE_MS)
  }

  // Prime cachedAgents immediately so /api/search works before first SSE tick
  getAgents().then((agents) => {
    cachedAgents = agents
  }).catch(() => {})

  discoverPatterns(getDb()).catch(err => consola.warn('Pattern discovery error:', err))

  function startSSEBroadcast() {
    if (sseBroadcastId)
      return
    sseBroadcastId = setInterval(async () => {
      try {
        const localAgents = await getAgents()
        cachedAgents = localAgents
        await broadcastAgents(localAgents)
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

  const taskSseClients = new Set<SseClient>()
  function broadcastTaskEvent(event: TaskEvent) {
    const data = `data: ${JSON.stringify(event)}\n\n`
    const task = getTaskById(event.taskId)
    // task_deleted: row already gone — fall back to userId embedded in event payload by taskRoutes
    const ownerId: string | null
      = task?.userId
        ?? (event.type === 'task_deleted' && event.payload != null && typeof event.payload === 'object' && 'userId' in event.payload
          ? String((event.payload as Record<string, unknown>).userId)
          : null)
    for (const client of taskSseClients) {
      if (!client.isAdmin && ownerId !== null && ownerId !== client.userId)
        continue
      try {
        if (!client.res.writableEnded)
          client.res.write(data)
      }
      catch {
        taskSseClients.delete(client)
      }
    }
  }

  app.get('/api/tasks/stream', (req, res) => {
    res.writeHead(200, {
      'Content-Type': 'text/event-stream',
      'Cache-Control': 'no-cache',
      'Connection': 'keep-alive',
      'X-Accel-Buffering': 'no',
    })
    res.flushHeaders()
    const client: SseClient = { res, userId: req.user!.id, isAdmin: req.user!.isAdmin }
    taskSseClients.add(client)
    req.on('close', () => {
      taskSseClients.delete(client)
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
      if (task) {
        broadcastTaskEvent({ type: 'task_updated', taskId, payload: enrichTask(task) })
        if (task.currentStage === 'done') {
          dispatcher
            .dispatch({
              eventType: 'completed',
              title: `Task "${task.title}" completed`,
              body: 'Pipeline task reached done stage',
              taskId,
              taskSlug: task.slug,
              severity: 'info',
            })
            .catch(err => consola.warn('[notifications] dispatch failed:', (err as Error).message))
        }
      }
      else {
        broadcastTaskEvent({ type: 'stage_run_updated', taskId, payload: info })
      }
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

  // Web Push VAPID subscription management routes
  app.use('/api', createWebPushRouter({ rejectCrossOrigin }))

  // Permission preset management routes (list/delete remembered tool grants per project)
  app.use('/api', createPresetRouter(rejectCrossOrigin))

  // Task pipeline routes (must come after rejectCrossOrigin definition)
  app.use('/api', createTaskRouter({
    rejectCrossOrigin,
    orchestrator,
    broadcastTaskEvent,
    dispatcher,
  }))

  // Refine routes (agent-based ticket refinement)
  app.use('/api/refine', createRefineRouter(
    (taskId) => {
      const task = getTaskById(taskId)
      if (task)
        broadcastTaskEvent({ type: 'task_updated', taskId, payload: enrichTask(task) })
    },
    rejectCrossOrigin,
  ))

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

  // Historical session import routes
  app.use('/api', createHistoryRouter())

  // Memory file browser routes
  app.use('/api', createMemoryRouter())

  // Full-text search across tasks (FTS5) and agents (in-memory)
  app.use('/api', createSearchRouter({ getAgents: () => cachedAgents }))

  // Agent routes (REST endpoints — non-SSE; SSE stream stays above)
  app.use('/api', createAgentRouter({ spawnManager, requireApiToken, rejectCrossOrigin }))

  // ─── HTTP server ───────────────────────────────────────

  const httpServer = createHttpServer(app)
  const isProd = process.env.NODE_ENV === 'production'

  if (isProd) {
    const distPath = join(import.meta.dirname, '..', 'dist')
    app.use(express.static(distPath))
    // SPA fallback: serve index.html for all non-API routes
    app.get('/{*splat}', (_req, res) => {
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
    consola.error('Unhandled route error', err)
    if (!res.headersSent) {
      const message = process.env.NODE_ENV === 'production'
        ? 'Internal server error'
        : (err instanceof Error ? err.message : 'Internal server error')
      res.status(500).json({ error: message })
    }
  })

  httpServer.listen(PORT, HOST, () => {
    const mode = isProd ? 'production' : 'development'
    consola.info(`Claude Agent Overview (${mode}) running at http://localhost:${PORT}`)
  })
}

start()
