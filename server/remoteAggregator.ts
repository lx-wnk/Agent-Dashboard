import { hostname } from 'node:os'

import { consola } from 'consola'

import type { Agent } from '../src/types.js'

const REMOTE_TIMEOUT_MS = 5000
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

async function fetchRemoteAgents(url: string): Promise<(Agent & { machine: string })[]> {
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), REMOTE_TIMEOUT_MS)

  try {
    const res = await fetch(`${url}/api/agents`, { signal: controller.signal })
    clearTimeout(timeout)
    if (!res.ok) {
      consola.warn(`[remotes] ${url} responded with HTTP ${res.status}`)
      return []
    }
    const data = await res.json()
    if (!Array.isArray(data)) {
      consola.warn(`[remotes] ${url} returned non-array response`)
      return []
    }
    const label = new URL(url).hostname
    return (data as Agent[]).map(a => ({ ...a, machine: label }))
  }
  catch (err) {
    clearTimeout(timeout)
    const reason = (err as Error).name === 'AbortError' ? 'timeout' : (err as Error).message
    consola.warn(`[remotes] Failed to reach ${url}: ${reason}`)
    return []
  }
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
