import type { PipelineStage } from '../src/types.js'

// Narrower than the DB CHECK constraint — old stages (pruefung, refinement,
// planning, approval1, umsetzungskonzept, approval2) are excluded intentionally.
// The DB CHECK stays broad so legacy rows survive migrations safely.
export const VALID_STAGES = new Set<PipelineStage>([
  'konzept',
  'backlog',
  'umsetzung',
  'selbstreview',
  'finalisierung',
  'done',
  'on_hold',
  'cancelled',
])

export const SLUG_RE = /^[a-z0-9][a-z0-9-]{0,63}$/
export const SLUG_PATTERN_MESSAGE = 'slug must match [a-z0-9][a-z0-9-]{0,63}'

export const SYSTEM_PROMPT_MAX_CHARS = 10_000

export const DEPENDENCY_REQUIRED_STAGES = ['done', 'cancelled'] as const
export const DEPENDENCY_CANCEL_ACTIONS = ['cancel', 'start', 'on_hold'] as const
