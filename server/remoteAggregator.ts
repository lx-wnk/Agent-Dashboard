import { hostname } from 'node:os'

import { consola } from 'consola'

import type { Agent } from '../src/types.js'

const REMOTE_TIMEOUT_MS = 5000
const localHostname = hostname()

export function getRemoteUrls(): string[] {
  const env = process.env.DASHBOARD_REMOTES
  if (!env)
    return []
  return env.split(',')
    .map(u => u.trim())
    .filter(Boolean)
    .filter((u) => {
      try {
        const parsed = new URL(u)
        return parsed.protocol === 'http:' || parsed.protocol === 'https:'
      }
      catch {
        consola.warn(`[remotes] Ignoring invalid URL: ${u}`)
        return false
      }
    })
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

export async function aggregateAgents(localAgents: Agent[]): Promise<Agent[]> {
  const remoteUrls = getRemoteUrls()
  if (remoteUrls.length === 0)
    return localAgents

  const tagged = localAgents.map(a => ({ ...a, machine: localHostname }))

  const remoteResults = await Promise.all(
    remoteUrls.map(url => fetchRemoteAgents(url)),
  )

  return [...tagged, ...remoteResults.flat()]
}
