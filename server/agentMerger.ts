import type { ProcessInfo } from './processScanner.js'
import type { SessionData } from './jsonlParser.js'
import type { Agent } from '../src/types.js'
import { findSessionForProject } from './jsonlParser.js'
import { scanProcesses } from './processScanner.js'
import { basename } from 'node:path'

const ACTIVE_THRESHOLD = 30_000    // 30s
const IDLE_THRESHOLD = 300_000     // 5min

function calculateStatus(lastActivity: string): Agent['status'] {
  const age = Date.now() - new Date(lastActivity).getTime()
  if (age < ACTIVE_THRESHOLD) return 'active'
  if (age < IDLE_THRESHOLD) return 'waiting'
  return 'idle'
}

export async function getAgents(): Promise<Agent[]> {
  const processes = await scanProcesses()
  const agents: Agent[] = []

  for (const proc of processes) {
    const session = await findSessionForProject(proc.cwd)

    const agent: Agent = {
      pid: proc.pid,
      sessionId: session?.sessionId || 'unknown',
      projectPath: proc.cwd,
      projectName: basename(proc.cwd),
      cwd: proc.cwd,
      entrypoint: session?.entrypoint || 'unknown',
      status: session?.lastActivity
        ? calculateStatus(session.lastActivity)
        : 'idle',
      uptime: proc.uptime,
      lastActivity: session?.lastActivity || new Date().toISOString(),
      currentAction: session?.currentAction || null,
      lastTools: session?.lastTools || [],
      tasks: session?.tasks || [],
      subagents: (session?.subagents || []).map(sa => ({
        ...sa,
        type: sa.type.length > 60 ? sa.type.substring(0, 60) + '...' : sa.type,
      })),
    }

    agents.push(agent)
  }

  // Sort: active first, then by uptime descending
  agents.sort((a, b) => {
    const statusOrder = { active: 0, waiting: 1, idle: 2 }
    const diff = statusOrder[a.status] - statusOrder[b.status]
    if (diff !== 0) return diff
    return b.uptime - a.uptime
  })

  return agents
}
