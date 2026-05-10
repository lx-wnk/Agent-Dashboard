import { AGENT_STATUSES, type AgentStatus } from '../types.js'

export const STATUS_ORDER = Object.fromEntries(
  AGENT_STATUSES.map((s, i) => [s, i]),
) as Record<AgentStatus, number>
