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
