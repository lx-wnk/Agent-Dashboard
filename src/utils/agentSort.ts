import type { AgentStatus } from '../types'
import { AGENT_STATUSES } from '../types'

export const STATUS_ORDER = Object.fromEntries(
  AGENT_STATUSES.map((s, i) => [s, i]),
) as Record<AgentStatus, number>
