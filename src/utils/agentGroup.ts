import type { Agent } from '../types'
import { AGENT_STATUSES } from '../types'
import { secondsSince, shortModel } from './format'
import { friendlyProjectName } from './friendlyProjectName'
import { STATUS_ORDER } from './agentSort'

export const AGENT_SORT_OPTIONS = [
  { value: 'latest', label: 'Latest active' },
  { value: 'longest', label: 'Longest running' },
  { value: 'expensive', label: 'Most expensive' },
] as const

export const AGENT_GROUP_OPTIONS = [
  { value: 'none', label: 'No grouping' },
  { value: 'project', label: 'Group by project' },
  { value: 'status', label: 'Group by status' },
  { value: 'model', label: 'Group by model' },
] as const

export type AgentSort = typeof AGENT_SORT_OPTIONS[number]['value']
export type AgentGroup = typeof AGENT_GROUP_OPTIONS[number]['value']

export interface AgentGrouping {
  key: string
  label: string | null
  agents: Agent[]
}

const STATUS_LABELS: Record<string, string> = {
  active: 'Active',
  waiting: 'Waiting on you',
  idle: 'Idle',
}

export function sortAgents(list: Agent[], sortBy: AgentSort, nowMs: number): Agent[] {
  const sorted = [...list]
  if (sortBy === 'longest') {
    sorted.sort((a, b) => b.uptime - a.uptime)
  }
  else if (sortBy === 'expensive') {
    sorted.sort((a, b) => b.costEstimate - a.costEstimate)
  }
  else {
    // latest: ascending seconds-since-activity = most-recently-active first
    sorted.sort((a, b) => {
      const sa = secondsSince(a.lastActivity, nowMs) ?? Infinity
      const sb = secondsSince(b.lastActivity, nowMs) ?? Infinity
      return sa - sb
    })
  }
  return sorted
}

export function groupAgents(list: Agent[], groupBy: AgentGroup): AgentGrouping[] {
  if (groupBy === 'project') {
    const seen = new Map<string, Agent[]>()
    for (const agent of list) {
      const key = agent.projectName
      const bucket = seen.get(key)
      if (bucket) {
        bucket.push(agent)
      }
      else {
        seen.set(key, [agent])
      }
    }
    return Array.from(seen.entries()).map(([key, agents]) => ({
      key,
      label: friendlyProjectName(key),
      agents,
    }))
  }

  if (groupBy === 'status') {
    return AGENT_STATUSES
      .slice()
      .sort((a, b) => STATUS_ORDER[a] - STATUS_ORDER[b])
      .map(s => ({ key: s, label: STATUS_LABELS[s] ?? s, agents: list.filter(a => a.status === s) }))
      .filter(g => g.agents.length > 0)
  }

  if (groupBy === 'model') {
    const seen = new Map<string, Agent[]>()
    for (const agent of list) {
      const key = shortModel(agent.model ?? null)
      const bucket = seen.get(key)
      if (bucket) {
        bucket.push(agent)
      }
      else {
        seen.set(key, [agent])
      }
    }
    return Array.from(seen.entries()).map(([key, agents]) => ({ key, label: key, agents }))
  }

  return [{ key: 'all', label: null, agents: list }]
}
