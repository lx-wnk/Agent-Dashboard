import { hostname } from 'node:os'

import type { Agent } from '../src/types.js'

const REMOTE_TIMEOUT_MS = 5000
const localHostname = hostname()

export function getRemoteUrls(): string[] {
  const env = process.env.DASHBOARD_REMOTES
  if (!env)
    return []
  return env.split(',').map(u => u.trim()).filter(Boolean)
}

async function fetchRemoteAgents(url: string): Promise<(Agent & { machine: string })[]> {
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), REMOTE_TIMEOUT_MS)

  try {
    const res = await fetch(`${url}/api/agents`, { signal: controller.signal })
    clearTimeout(timeout)
    if (!res.ok)
      return []
    const agents: Agent[] = await res.json()
    const label = new URL(url).hostname
    return agents.map(a => ({ ...a, machine: label }))
  }
  catch {
    clearTimeout(timeout)
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
