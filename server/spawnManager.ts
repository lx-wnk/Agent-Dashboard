/**
 * SpawnManager — encapsulates dashboard-initiated Claude agent spawns,
 * their in-memory status store, rate limiting, stderr ring-buffer, and
 * the channel-reply store. Used by `server/index.ts` to keep the
 * Express bootstrap lean.
 *
 * Pure extraction of previously inline logic — no behavioral changes.
 * See server/pipeline/agentSpawner.ts for the pipeline-driven spawn path
 * (separate flow, distinct responsibilities).
 */
import type { Buffer } from 'node:buffer'
import type { ChannelInfo } from './channelDiscovery.js'
import { spawn } from 'node:child_process'
import { existsSync } from 'node:fs'
import process from 'node:process'
import { consola } from 'consola'
import { buildDashboardChannelMcpConfig } from './channelConfig.js'
import { SYSTEM_PROMPT_MAX_CHARS } from './constants.js'

const UUID_RE = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/i
const ALLOWED_MODELS = new Set(['claude-opus-4-6', 'claude-sonnet-4-6', 'claude-haiku-4-5', '']) // empty string = "Auto" (no --model flag)

const SPAWN_STORE_MAX_AGE_MS = 60 * 60 * 1000 // 1 hour
const MAX_REPLIES_PER_PID = 50
const MAX_STDERR_BYTES = 4096

// Rate limit: sliding window, max spawn requests
const RATE_LIMIT_WINDOW_MS = (() => {
  const val = Number(process.env.DASHBOARD_SPAWN_RATE_WINDOW_MS ?? 60_000)
  if (!Number.isFinite(val) || val <= 0)
    throw new Error(`DASHBOARD_SPAWN_RATE_WINDOW_MS must be a positive integer (got: ${process.env.DASHBOARD_SPAWN_RATE_WINDOW_MS})`)
  return val
})()
const RATE_LIMIT_MAX = (() => {
  const val = Number(process.env.DASHBOARD_SPAWN_RATE_LIMIT ?? 5)
  if (!Number.isFinite(val) || val <= 0)
    throw new Error(`DASHBOARD_SPAWN_RATE_LIMIT must be a positive integer (got: ${process.env.DASHBOARD_SPAWN_RATE_LIMIT})`)
  return val
})()

// Per-channel fetch timeout when forwarding a user message
const CHANNEL_MESSAGE_TIMEOUT_MS = 5000

export interface SpawnStatus {
  pid: number
  status: 'running' | 'exited' | 'error'
  exitCode: number | null
  stderr: string
  startedAt: string
  prompt: string
  cwd: string
}

export interface SpawnRequest {
  prompt?: unknown
  cwd?: unknown
  model?: unknown
  systemPrompt?: unknown
  enableChannel?: unknown
  skipPermissions?: unknown
  resumeSessionId?: unknown
}

export type SpawnResult
  = | { ok: true, pid: number }
    | { ok: false, status: number, error: string }

export interface Reply {
  message: string
  timestamp: string
}

export type ChannelDispatchResult
  = | { kind: 'response', status: number, body: unknown }
    | { kind: 'timeout' }
    | { kind: 'unreachable', message: string }

/**
 * Narrow shape of the Agent we actually use inside
 * `sendMessageToChannel`. Accepting just these fields keeps the
 * manager decoupled from the full Agent type surface.
 */
export interface AgentForChannel {
  pid: number
  cwd: string
  channelAvailable: boolean
}

export class SpawnManager {
  private readonly spawnStore = new Map<number, SpawnStatus>()
  private readonly replyStore = new Map<number, Reply[]>()
  private readonly spawnTimestamps: number[] = []

  /**
   * Prune old timestamps and check whether another spawn is allowed.
   * Call `recordSpawnAttempt()` after a successful allow to consume a slot.
   */
  isSpawnAllowed(now: number = Date.now()): boolean {
    const windowStart = now - RATE_LIMIT_WINDOW_MS
    while (this.spawnTimestamps.length > 0 && this.spawnTimestamps[0] <= windowStart) {
      this.spawnTimestamps.shift()
    }
    return this.spawnTimestamps.length < RATE_LIMIT_MAX
  }

  /**
   * Return the current rate limit configuration (for error messages).
   */
  getRateLimitConfig(): { windowMs: number, max: number } {
    return { windowMs: RATE_LIMIT_WINDOW_MS, max: RATE_LIMIT_MAX }
  }

  private recordSpawnAttempt(now: number = Date.now()): void {
    this.spawnTimestamps.push(now)
  }

  /**
   * Validate input, spawn the Claude CLI as a detached process, wire up
   * stderr ring-buffer / exit / error handlers, and register the entry
   * in the spawn store. Rate-limiting must be checked by the caller via
   * `isSpawnAllowed()` first — this method consumes a slot unconditionally.
   */
  spawnAgent(body: SpawnRequest): SpawnResult {
    // Consume a rate-limit slot up-front — matches pre-extraction semantics
    // where even requests that fail validation counted against the window.
    this.recordSpawnAttempt()

    const { prompt, cwd, model, systemPrompt, enableChannel, skipPermissions, resumeSessionId } = body

    if (!prompt || typeof prompt !== 'string') {
      return { ok: false, status: 400, error: 'Missing or invalid "prompt" field' }
    }
    if (!cwd || typeof cwd !== 'string') {
      return { ok: false, status: 400, error: 'Missing or invalid "cwd" field' }
    }
    if (!existsSync(cwd)) {
      return { ok: false, status: 400, error: `Directory does not exist: ${cwd}` }
    }
    if (model !== undefined && model !== null && model !== '' && typeof model !== 'string') {
      return { ok: false, status: 400, error: 'Invalid model' }
    }
    if (typeof model === 'string' && !ALLOWED_MODELS.has(model)) {
      return { ok: false, status: 400, error: 'Invalid model' }
    }
    if (resumeSessionId !== undefined && resumeSessionId !== null && resumeSessionId !== '') {
      if (typeof resumeSessionId !== 'string' || !UUID_RE.test(resumeSessionId)) {
        return { ok: false, status: 400, error: 'Invalid sessionId format' }
      }
    }

    try {
      const args: string[] = []
      if (skipPermissions) {
        args.push('--dangerously-skip-permissions')
      }
      if (typeof resumeSessionId === 'string' && resumeSessionId) {
        args.push('--resume', resumeSessionId)
      }
      args.push('-p', prompt)
      if (typeof model === 'string' && model) {
        args.push('--model', model)
      }
      if (systemPrompt && typeof systemPrompt === 'string') {
        args.push('--system-prompt', systemPrompt.slice(0, SYSTEM_PROMPT_MAX_CHARS))
      }
      if (enableChannel !== false) {
        args.push('--mcp-config', buildDashboardChannelMcpConfig())
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
      this.spawnStore.set(pid, status)

      // Collect stderr into a bounded ring-buffer
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
        for (const [key, entry] of this.spawnStore) {
          if (Date.now() - new Date(entry.startedAt).getTime() > SPAWN_STORE_MAX_AGE_MS) {
            this.spawnStore.delete(key)
          }
        }
      })

      child.on('error', (err) => {
        status.status = 'error'
        status.stderr += `\nSpawn error: ${err.message}`
        console.error(`[spawn] PID ${pid} error:`, err)
      })

      child.unref()
      return { ok: true, pid }
    }
    catch (err) {
      console.error('Error spawning agent:', err)
      return { ok: false, status: 500, error: 'Internal error' }
    }
  }

  /** Lookup a spawn status by PID. Returns undefined if unknown. */
  getStatus(pid: number): SpawnStatus | undefined {
    return this.spawnStore.get(pid)
  }

  /**
   * Append a reply to the ring-buffer for `pid`. Oldest entries are
   * discarded beyond `MAX_REPLIES_PER_PID`.
   */
  storeReply(pid: number, message: string, timestamp: string): void {
    let replies = this.replyStore.get(pid)
    if (!replies) {
      replies = []
      this.replyStore.set(pid, replies)
    }
    replies.push({ message, timestamp })
    if (replies.length > MAX_REPLIES_PER_PID) {
      replies.shift()
    }
  }

  /**
   * Return replies for `pid`, optionally filtered to those with a
   * timestamp strictly after `since` (ISO 8601 string).
   */
  getReplies(pid: number, since?: string): Reply[] {
    const replies = this.replyStore.get(pid) || []
    if (!since)
      return replies
    const sinceTime = new Date(since).getTime()
    return replies.filter(r => new Date(r.timestamp).getTime() > sinceTime)
  }

  /**
   * Forward a user message to an agent's channel MCP server. Tries a
   * PID-based lookup first, falls back to a cwd match. Returns a
   * structured result the caller can map onto an HTTP response.
   */
  async sendMessageToChannel(
    agent: AgentForChannel,
    message: string,
    channelMap: Map<number, ChannelInfo>,
  ): Promise<ChannelDispatchResult | { kind: 'not_found' }> {
    if (!agent.channelAvailable)
      return { kind: 'not_found' }

    let channel = channelMap.get(agent.pid)
    if (!channel) {
      for (const [, info] of channelMap) {
        if (info.cwd && info.cwd === agent.cwd) {
          channel = info
          break
        }
      }
    }
    if (!channel)
      return { kind: 'not_found' }

    const controller = new AbortController()
    const timeout = setTimeout(() => controller.abort(), CHANNEL_MESSAGE_TIMEOUT_MS)

    try {
      const response = await fetch(`http://127.0.0.1:${channel.port}/message`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message }),
        signal: controller.signal,
      })
      clearTimeout(timeout)
      const data = await response.json()
      return { kind: 'response', status: response.status, body: data }
    }
    catch (err) {
      clearTimeout(timeout)
      if ((err as Error).name === 'AbortError')
        return { kind: 'timeout' }
      return { kind: 'unreachable', message: (err as Error).message }
    }
  }
}
