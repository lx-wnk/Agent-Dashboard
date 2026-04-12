import type { PermissionRequest, PipelineStage, PipelineTask, StageRun, TaskPermission } from '../../src/types.js'

export type StageTransition
  = | { kind: 'next', toStage: PipelineStage, output?: Record<string, unknown> }
    | { kind: 'wait_user', reason: string, output?: Record<string, unknown> }
    | { kind: 'iterate', output?: Record<string, unknown> }
    | { kind: 'on_hold', permissionRequestId: string, output?: Record<string, unknown> }
    | { kind: 'done', output?: Record<string, unknown> }
    | { kind: 'fail', error: string, output?: Record<string, unknown> }

export interface StageContext {
  task: PipelineTask
  stageRun: StageRun
  permissions: TaskPermission[]
  previousOutput: Record<string, unknown> | null
  // Injected by orchestrator for side effects:
  recordAudit: (action: string, details?: Record<string, unknown>) => void
  requestPermission: (tool: string, pattern: string | null, reason: string) => PermissionRequest
}

export interface StageHandler {
  readonly stage: PipelineStage
  readonly requiresAgent: boolean
  execute: (ctx: StageContext) => Promise<StageTransition>
}

// Canonical stage order for auto-transitions
export const STAGE_ORDER: PipelineStage[] = [
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
]

export function nextStage(stage: PipelineStage): PipelineStage | null {
  const idx = STAGE_ORDER.indexOf(stage)
  if (idx === -1 || idx === STAGE_ORDER.length - 1)
    return null
  return STAGE_ORDER[idx + 1]
}
