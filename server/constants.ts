import type { PipelineStage } from '../src/types.js'

export const VALID_STAGES = new Set<PipelineStage>([
  'backlog',
  'pruefung',
  'refinement',
  'planning',
  'approval1',
  'umsetzungskonzept',
  'approval2',
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
