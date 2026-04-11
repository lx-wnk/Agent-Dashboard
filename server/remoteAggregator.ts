import { hostname } from 'node:os'

import { consola } from 'consola'

import type { Agent } from '../src/types.js'

const REMOTE_TIMEOUT_MS = 5000
const MAX_RESPONSE_BYTES = 1_048_576 // 1MB
const ORIGIN_HEADER = 'X-Dashboard-Origin'
const VALID_STATUSES = new Set(['active', 'waiting', 'idle'])
const UUID_RE = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/i
const localHostname = hostname()

export function getRemoteUrls(): string[] {
  const env = process.env.DASHBOARD_REMOTES
  if (!env)
    return []
  const urls = env.split(',')
    .map(u => u.trim())
    .filter(Boolean)
    .filter((u) => {
      try {
        const parsed = new URL(u)
        if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:')
          return false
        // Filter out self-referencing URLs to prevent fetch loops
        const selfHosts = ['localhost', '127.0.0.1', '[::1]', '0.0.0.0']
        if (selfHosts.includes(parsed.hostname) && parsed.port === String(process.env.DASHBOARD_PORT || '13120')) {
          consola.warn(`[remotes] Skipping ${u} — points to this dashboard instance`)
          return false
        }
        return true
      }
      catch {
        consola.warn(`[remotes] Ignoring invalid URL: ${u}`)
        return false
      }
    })
  return [...new Set(urls)]
}

function validateAgent(obj: unknown): obj is Agent {
  if (typeof obj !== 'object' || obj === null) return false
  const a = obj as Record<string, unknown>
  return typeof a.pid === 'number'
    && typeof a.sessionId === 'string' && UUID_RE.test(a.sessionId)
    && typeof a.status === 'string' && VALID_STATUSES.has(a.status)
    && typeof a.projectPath === 'string'
    && typeof a.projectName === 'string'
}

async function fetchRemoteAgents(url: string): Promise<(Agent & { machine: string })[]> {
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), REMOTE_TIMEOUT_MS)

  try {
    const res = await fetch(`${url}/api/agents`, {
      signal: controller.signal,
      headers: { [ORIGIN_HEADER]: localHostname },
    })
    clearTimeout(timeout)
    if (!res.ok) {
      consola.warn(`[remotes] ${url} responded with HTTP ${res.status}`)
      return []
    }

    // Enforce response size limit
    const contentLength = res.headers.get('content-length')
    if (contentLength && Number.parseInt(contentLength, 10) > MAX_RESPONSE_BYTES) {
      consola.warn(`[remotes] ${url} response too large (${contentLength} bytes)`)
      return []
    }

    const text = await res.text()
    if (text.length > MAX_RESPONSE_BYTES) {
      consola.warn(`[remotes] ${url} response too large (${text.length} bytes)`)
      return []
    }

    const data = JSON.parse(text)
    if (!Array.isArray(data)) {
      consola.warn(`[remotes] ${url} returned non-array response`)
      return []
    }

    const label = new URL(url).hostname
    return data
      // Skip agents already tagged with a machine (prevent transitive chains)
      .filter((a: Record<string, unknown>) => !a.machine)
      // Validate required fields
      .filter((a: unknown) => {
        if (!validateAgent(a)) {
          consola.debug(`[remotes] Skipping invalid agent from ${url}`)
          return false
        }
        return true
      })
      .map((a: Agent) => ({ ...a, machine: label }))
  }
  catch (err) {
    clearTimeout(timeout)
    const reason = (err as Error).name === 'AbortError' ? 'timeout' : (err as Error).message
    consola.warn(`[remotes] Failed to reach ${url}: ${reason}`)
    return []
  }
}

/**
 * Check if the current request is a forwarded remote fetch (to prevent re-aggregation).
 */
export function isRemoteFetch(reqHeaders: Record<string, string | string[] | undefined>): boolean {
  return !!reqHeaders[ORIGIN_HEADER.toLowerCase()]
}

export async function aggregateAgents(localAgents: Agent[], remoteUrls: string[]): Promise<Agent[]> {
  if (remoteUrls.length === 0)
    return localAgents

  const tagged = localAgents.map(a => ({ ...a, machine: localHostname }))

  const remoteResults = await Promise.all(
    remoteUrls.map(url => fetchRemoteAgents(url)),
  )

  return [...tagged, ...remoteResults.flat()]
}
