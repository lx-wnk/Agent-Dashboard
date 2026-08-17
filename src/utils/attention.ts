import type { Agent } from '../types'
import { isStalled } from './format'

export interface Attention {
  kind: 'question' | 'permission' | 'error' | 'stalled'
  label: string
  tone: 'warning' | 'danger'
  weight: number
}

export function attentionFor(agent: Agent, secondsSinceActivity: number | null): Attention | null {
  // A finished agent's process is gone: its reconstructed errorState/pendingToolUse
  // are historical and not actionable from the triage band (dismiss lives on the card).
  if (agent.status === 'finished')
    return null
  // A real, answerable AskUserQuestion outranks a generic permission prompt.
  if (agent.pendingQuestion)
    return { kind: 'question', label: 'Question', tone: 'warning', weight: -1 }
  // The review/submit screen is equally answerable and equally blocking: the
  // session sits there until someone presses a key.
  if (agent.pendingConfirm)
    return { kind: 'question', label: 'Confirm answers', tone: 'warning', weight: -1 }
  if (agent.pendingPermissions && agent.pendingPermissions.length > 0)
    return { kind: 'permission', label: 'Needs permission', tone: 'warning', weight: 0 }
  // An unresolved tool_use is the only signal a plain JSONL gives for "blocked on
  // a permission prompt" — but a session that never prompts produces the same
  // shape while a tool is simply still running. Reporting that as attention
  // buried the real prompts in noise.
  if (agent.pendingToolUse && !agent.permissionsBypassed)
    return { kind: 'permission', label: 'Needs permission', tone: 'warning', weight: 0 }
  if (agent.errorState)
    return { kind: 'error', label: 'Run failed', tone: 'danger', weight: 1 }
  if (isStalled(agent.status, secondsSinceActivity))
    return { kind: 'stalled', label: 'No activity', tone: 'warning', weight: 2 }
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
