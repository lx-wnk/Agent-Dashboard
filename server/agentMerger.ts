import type { Agent, TokenUsage } from '../src/types.js'
import { basename } from 'node:path'
import { getChannelMap } from './channelDiscovery.js'
import { findTasksBySessionIds } from './db/stageRunsRepo.js'
import { findSessionForProject } from './jsonlParser.js'
import { estimateCost } from './pricing.js'
import { scanProcesses } from './processScanner.js'

const ACTIVE_THRESHOLD = 30_000 // 30s
const IDLE_THRESHOLD = 300_000 // 5min

export function enrichWithPipelineTask(agents: Agent[]): void {
  if (agents.length === 0)
    return
  try {
    const taskMap = findTasksBySessionIds(agents.map(a => a.sessionId))
    for (const agent of agents) {
      const entry = taskMap.get(agent.sessionId)
      if (entry) {
        agent.pipelineTaskId = entry.taskId
        agent.pipelineTaskTitle = entry.title
      }
    }
  }
  catch (err) {
    // Opportunistic enrichment — skip if pipeline DB is unavailable (e.g. first boot, missing schema)
    const msg = err instanceof Error ? err.message : String(err)
    const isExpected = msg.includes('no such table') || msg.includes('ENOENT') || msg.includes('no such file')
    if (!isExpected)
      console.warn('[agentMerger] enrichWithPipelineTask failed:', err)
  }
}

export function calculateStatus(lastActivity: string): Agent['status'] {
  const age = Date.now() - new Date(lastActivity).getTime()
  if (age < ACTIVE_THRESHOLD)
    return 'active'
  if (age < IDLE_THRESHOLD)
    return 'waiting'
  return 'idle'
}

export async function getAgents(): Promise<Agent[]> {
  const processes = await scanProcesses()

  const sessions = await Promise.all(
    processes.map(proc => findSessionForProject(proc.cwd)),
  )

  const agents: Agent[] = processes.map((proc, i) => {
    const session = sessions[i]

    const tokenUsage: TokenUsage = session?.tokenUsage
      ? { ...session.tokenUsage }
      : { inputTokens: 0, outputTokens: 0, cacheCreationTokens: 0, cacheReadTokens: 0 }

    return {
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
      tasks: (session?.tasks || []) as Agent['tasks'],
      subagents: (session?.subagents || []).map(sa => ({
        ...sa,
        type: sa.type.length > 60 ? `${sa.type.substring(0, 60)}...` : sa.type,
      })),
      tokenUsage,
      costEstimate: estimateCost(session?.tokenUsage || tokenUsage, session?.model || null),
      model: session?.model || null,
      codeVersion: session?.codeVersion || null,
      conversationTurns: session?.conversationTurns || 0,
      toolCounts: session?.toolCounts || {},
      meta: session?.meta || null,
      lastOutput: session?.lastOutput ?? null,
      lastBtw: session?.lastBtw ?? null,
      channelAvailable: false, // set after channel discovery below
    }
  })

  // Annotate agents that have a channel available
  const channelMap = await getChannelMap()
  for (const agent of agents) {
    if (channelMap.has(agent.pid)) {
      agent.channelAvailable = true
    }
    else {
      // Fallback: match by cwd if PID-based matching failed
      for (const [, info] of channelMap) {
        if (info.cwd && info.cwd === agent.cwd) {
          agent.channelAvailable = true
          break
        }
      }
    }
  }

  // Sort: active first, then by uptime descending
  agents.sort((a, b) => {
    const statusOrder = { active: 0, waiting: 1, idle: 2 }
    const diff = statusOrder[a.status] - statusOrder[b.status]
    if (diff !== 0)
      return diff
    return b.uptime - a.uptime
  })

  enrichWithPipelineTask(agents)

  return agents
}
