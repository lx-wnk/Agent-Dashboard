import type { ProcessInfo } from './processScanner.js'
import type { SessionData, TokenUsageData } from './jsonlParser.js'
import type { Agent, TokenUsage } from '../src/types.js'
import { findSessionForProject } from './jsonlParser.js'
import { scanProcesses } from './processScanner.js'
import { basename } from 'node:path'

const ACTIVE_THRESHOLD = 30_000    // 30s
const IDLE_THRESHOLD = 300_000     // 5min

// Pricing per 1M tokens (USD) — Claude Code Pro/Max users pay via subscription,
// but we estimate API-equivalent cost for visibility
const MODEL_PRICING: Record<string, { input: number; output: number; cacheRead: number; cacheCreate: number }> = {
  'claude-opus-4-6':   { input: 15, output: 75, cacheRead: 1.5, cacheCreate: 18.75 },
  'claude-opus-4-0':   { input: 15, output: 75, cacheRead: 1.5, cacheCreate: 18.75 },
  'claude-sonnet-4-6': { input: 3,  output: 15, cacheRead: 0.3, cacheCreate: 3.75 },
  'claude-sonnet-4-5': { input: 3,  output: 15, cacheRead: 0.3, cacheCreate: 3.75 },
  'claude-haiku-4-5':  { input: 0.8, output: 4, cacheRead: 0.08, cacheCreate: 1 },
}

function estimateCost(usage: TokenUsageData, model: string | null): number {
  const pricing = (model && MODEL_PRICING[model]) || MODEL_PRICING['claude-sonnet-4-6']
  const m = 1_000_000
  return (
    (usage.inputTokens * pricing.input) / m +
    (usage.outputTokens * pricing.output) / m +
    (usage.cacheReadTokens * pricing.cacheRead) / m +
    (usage.cacheCreationTokens * pricing.cacheCreate) / m
  )
}

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

    const tokenUsage: TokenUsage = session?.tokenUsage
      ? { ...session.tokenUsage }
      : { inputTokens: 0, outputTokens: 0, cacheCreationTokens: 0, cacheReadTokens: 0 }

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
      tasks: (session?.tasks || []) as Agent['tasks'],
      subagents: (session?.subagents || []).map(sa => ({
        ...sa,
        type: sa.type.length > 60 ? sa.type.substring(0, 60) + '...' : sa.type,
      })),
      tokenUsage,
      costEstimate: estimateCost(session?.tokenUsage || tokenUsage, session?.model || null),
      model: session?.model || null,
      codeVersion: session?.codeVersion || null,
      conversationTurns: session?.conversationTurns || 0,
      toolCounts: session?.toolCounts || {},
      meta: session?.meta || null,
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
