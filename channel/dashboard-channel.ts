/**
 * Dashboard Channel MCP Server
 *
 * Spawned by Claude Code as a subprocess via:
 *   claude --dangerously-load-development-channels server:dashboard-channel
 *
 * Architecture:
 *   1. MCP Server on stdio ← communicates with Claude Code
 *   2. HTTP Server on localhost (random port) ← receives messages from dashboard
 *   3. Discovery file at ~/.claude/dashboard-channel/{parentPid}.json
 */
import { Server } from '@modelcontextprotocol/sdk/server/index.js'
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js'
import {
  ListToolsRequestSchema,
  CallToolRequestSchema,
} from '@modelcontextprotocol/sdk/types.js'
import { createServer } from 'node:http'
import { writeFileSync, mkdirSync, unlinkSync } from 'node:fs'
import { execFileSync } from 'node:child_process'
import { join } from 'node:path'
import { homedir } from 'node:os'
import { randomBytes } from 'node:crypto'

/**
 * Walk up the process tree to find the `claude` CLI process PID.
 * Claude Code spawns MCP servers via an intermediate `node` wrapper,
 * so process.ppid is the wrapper — not the claude PID that `ps` reports.
 */
function findClaudePid(): number {
  let pid = process.ppid
  try {
    for (let i = 0; i < 5; i++) {
      const out = execFileSync('ps', ['-o', 'ppid=,comm=', '-p', String(pid)], { encoding: 'utf-8' }).trim()
      const match = out.match(/^\s*(\d+)\s+(.+)$/)
      if (!match) break
      const comm = match[2]
      if (comm.endsWith('/claude') || comm === 'claude') return pid
      pid = parseInt(match[1], 10)
      if (pid <= 1) break
    }
  } catch { /* fallback to direct parent */ }
  return process.ppid
}

// ─── Config ──────────────────────────────────────────────
const DASHBOARD_PORT = 13120
const DISCOVERY_DIR = join(homedir(), '.claude', 'dashboard-channel')
const PARENT_PID = findClaudePid()
const TOKEN = randomBytes(16).toString('hex')

let discoveryPath: string | null = null

// ─── MCP Server ──────────────────────────────────────────

const CHANNEL_INSTRUCTIONS = `
Messages from the monitoring dashboard arrive as <channel source="dashboard" type="instruction">.
These are instructions or questions from the user watching your activity in a separate browser UI.

## How to handle dashboard messages

1. FINISH your current action (tool call, edit, etc.) before addressing the message.
   Never abandon a half-finished operation — complete it cleanly, then handle the instruction.

2. ACKNOWLEDGE immediately using the dashboard_reply tool with a short status:
   "Understood, will [action] after current step." (keep under 100 chars)

3. ASSESS priority yourself. The message may be:
   - A course correction ("stop working on X, focus on Y instead")
   - A supplementary hint ("also consider Z")
   - A question ("what is the status of X?")
   - A directive ("run the tests now")
   Use your judgment to decide whether to interrupt your plan or weave it in.

4. REPORT progress after each significant step (file edit, test run, tool completion)
   using dashboard_reply. Keep updates short and technical:
   "Edited auth.ts — running tests next."
   "Tests passed (14/14). Moving to refactor."

5. REPORT completion with a final dashboard_reply summarizing what you did:
   "Done: refactored auth module, 3 files changed, tests green."

## Reply format rules
- Use dashboard_reply for ALL communication back to the dashboard.
- Keep replies under 200 chars. No markdown, no code blocks, just plain status text.
- Never skip the initial acknowledgment — the user needs to see their message arrived.
`.trim()

const mcp = new Server(
  { name: 'dashboard-channel', version: '0.1.0' },
  {
    capabilities: {
      experimental: { 'claude/channel': {} },
      tools: {},
    },
    instructions: CHANNEL_INSTRUCTIONS,
  },
)

// ─── Reply Tool ──────────────────────────────────────────
// Claude calls this to send a message back to the dashboard

mcp.setRequestHandler(ListToolsRequestSchema, async () => ({
  tools: [
    {
      name: 'dashboard_reply',
      description:
        'Send a reply back to the monitoring dashboard. Use this when you have completed an instruction from the dashboard or want to report status/progress.',
      inputSchema: {
        type: 'object' as const,
        properties: {
          message: {
            type: 'string',
            description: 'The reply message to display in the dashboard',
          },
        },
        required: ['message'],
      },
    },
  ],
}))

mcp.setRequestHandler(CallToolRequestSchema, async (req) => {
  if (req.params.name === 'dashboard_reply') {
    const { message } = req.params.arguments as { message: string }

    try {
      const res = await fetch(`http://127.0.0.1:${DASHBOARD_PORT}/api/channel-reply`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${TOKEN}`,
        },
        body: JSON.stringify({
          parentPid: PARENT_PID,
          message,
          timestamp: new Date().toISOString(),
        }),
      })

      if (!res.ok) {
        return { content: [{ type: 'text', text: `Dashboard reply failed: ${res.status}` }] }
      }

      return { content: [{ type: 'text', text: 'Reply sent to dashboard.' }] }
    } catch (err) {
      return {
        content: [{ type: 'text', text: `Could not reach dashboard: ${(err as Error).message}` }],
      }
    }
  }

  return { content: [{ type: 'text', text: `Unknown tool: ${req.params.name}` }] }
})

// ─── HTTP Server (receives messages from dashboard) ──────

function readBody(req: import('node:http').IncomingMessage): Promise<string> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = []
    req.on('data', (c) => chunks.push(c))
    req.on('end', () => resolve(Buffer.concat(chunks).toString()))
    req.on('error', reject)
  })
}

const httpServer = createServer(async (req, res) => {
  // CORS preflight
  if (req.method === 'OPTIONS') {
    res.writeHead(204, {
      'Access-Control-Allow-Origin': '*',
      'Access-Control-Allow-Methods': 'POST, GET',
      'Access-Control-Allow-Headers': 'Content-Type',
    })
    res.end()
    return
  }

  const url = new URL(req.url || '/', `http://127.0.0.1`)

  // Health check
  if (req.method === 'GET' && url.pathname === '/health') {
    res.writeHead(200, { 'Content-Type': 'application/json' })
    res.end(JSON.stringify({ status: 'ok', parentPid: PARENT_PID }))
    return
  }

  // Receive message from dashboard → forward to Claude
  if (req.method === 'POST' && url.pathname === '/message') {
    try {
      const body = JSON.parse(await readBody(req))
      const message = body.message as string

      if (!message || typeof message !== 'string') {
        res.writeHead(400, { 'Content-Type': 'application/json' })
        res.end(JSON.stringify({ error: 'Missing "message" field' }))
        return
      }

      await mcp.notification({
        method: 'notifications/claude/channel',
        params: {
          content: message,
          meta: { source: 'dashboard', type: 'instruction' },
        },
      })

      res.writeHead(200, { 'Content-Type': 'application/json' })
      res.end(JSON.stringify({ ok: true }))
    } catch (err) {
      res.writeHead(500, { 'Content-Type': 'application/json' })
      res.end(JSON.stringify({ error: (err as Error).message }))
    }
    return
  }

  res.writeHead(404)
  res.end('Not found')
})

// ─── Discovery File ──────────────────────────────────────

function writeDiscovery(port: number) {
  mkdirSync(DISCOVERY_DIR, { recursive: true })
  discoveryPath = join(DISCOVERY_DIR, `${PARENT_PID}.json`)
  writeFileSync(
    discoveryPath,
    JSON.stringify({
      port,
      channelPid: process.pid,
      parentPid: PARENT_PID,
      cwd: process.cwd(),
      token: TOKEN,
      startedAt: new Date().toISOString(),
    }),
  )
}

function cleanup() {
  if (discoveryPath) {
    try {
      unlinkSync(discoveryPath)
    } catch {
      // File may already be gone
    }
  }
}

process.on('SIGTERM', () => { cleanup(); process.exit(0) })
process.on('SIGINT', () => { cleanup(); process.exit(0) })
process.on('exit', cleanup)

// ─── Startup ─────────────────────────────────────────────

httpServer.listen(0, '127.0.0.1', async () => {
  const addr = httpServer.address()
  const port = typeof addr === 'object' && addr ? addr.port : 0

  writeDiscovery(port)

  // Connect MCP over stdio to Claude Code
  const transport = new StdioServerTransport()
  await mcp.connect(transport)

  // Log to stderr (stdout is reserved for MCP stdio)
  console.error(`[dashboard-channel] HTTP on port ${port}, parent PID ${PARENT_PID}`)
})
