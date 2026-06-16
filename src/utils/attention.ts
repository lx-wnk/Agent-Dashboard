import type { Agent } from '../types'
import { isStalled } from './format'

export interface Attention {
  kind: 'permission' | 'error' | 'idle' | 'stalled'
  label: string
  tone: 'warning' | 'danger' | 'info'
  weight: number
}

export function attentionFor(agent: Agent, secondsSinceActivity: number | null): Attention | null {
  if (agent.status === 'waiting')
    return { kind: 'permission', label: 'Needs permission', tone: 'warning', weight: 0 }
  // errorState present on any status signals a failed run (auth/quota/rate-limit)
  if (agent.errorState)
    return { kind: 'error', label: 'Run failed', tone: 'danger', weight: 1 }
  if (isStalled(agent.status, secondsSinceActivity))
    return { kind: 'stalled', label: 'No activity', tone: 'warning', weight: 2 }
  if (agent.status === 'idle')
    return { kind: 'idle', label: 'Idle — awaiting instruction', tone: 'info', weight: 3 }
  return null
}

export function needsAttention(agent: Agent, secondsSinceActivity: number | null): boolean {
  return attentionFor(agent, secondsSinceActivity) !== null
}

// Sorts attention-needing agents first (by ascending weight), then by longer-waiting first.
// Non-attention agents are appended in their original order.
export function sortByTriage(agents: Agent[], secsOf: (a: Agent) => number | null): Agent[] {
  const attention: Agent[] = []
  const rest: Agent[] = []

  for (const agent of agents) {
    const secs = secsOf(agent)
    if (needsAttention(agent, secs))
      attention.push(agent)
    else
      rest.push(agent)
  }

  attention.sort((a, b) => {
    const attA = attentionFor(a, secsOf(a))!
    const attB = attentionFor(b, secsOf(b))!
    if (attA.weight !== attB.weight)
      return attA.weight - attB.weight
    // Longer-waiting first: higher secondsSince = more urgent
    const secsA = secsOf(a) ?? 0
    const secsB = secsOf(b) ?? 0
    return secsB - secsA
  })

  return [...attention, ...rest]
}
