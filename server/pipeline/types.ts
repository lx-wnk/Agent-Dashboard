import type { PermissionRequest, PipelineStage, PipelineTask, StageRun, TaskPermission } from '../../src/types.js'

export type StageTransition
  = | {
    kind: 'next'
    toStage: PipelineStage
    output?: Record<string, unknown>
    /**
     * Optional task.metadata replacement applied in the SAME transaction
     *  as the stage transition. Use this when a transition needs to mutate
     *  task-level state atomically — e.g. self_review stashing review
     *  feedback when looping back to implementation. Pass `null` to clear
     *  metadata entirely. Omit to leave metadata untouched.
     */
    taskMetadataPatch?: Record<string, unknown> | null
  }
  | { kind: 'wait_user', reason: string, output?: Record<string, unknown> }
  | { kind: 'iterate', output?: Record<string, unknown> }
  | { kind: 'on_hold', permissionRequestId: string, output?: Record<string, unknown> }
  | { kind: 'done', output?: Record<string, unknown> }
  | { kind: 'fail', error: string, output?: Record<string, unknown> }
  | { kind: 'async_running', pid: number, output?: Record<string, unknown> }

export interface StageContext {
  task: PipelineTask
  stageRun: StageRun
  permissions: TaskPermission[]
  previousOutput: Record<string, unknown> | null
  /**
   * Output of the prior iteration of the SAME stage, or null on iteration 0.
   *  Used by agent handlers to surface validation feedback to the retry
   *  prompt so the agent can self-correct a rejected schema.
   */
  priorIterationOutput: Record<string, unknown> | null
  /**
   * If set, the stage handler should pass this through to the agent spawn
   * as `--resume <sessionId>` so the new claude process continues from the
   * prior session's transcript instead of starting a fresh conversation.
   * Used by the resume-stage endpoint after a permission grant.
   */
  resumeSessionId?: string
  /**
   * Optional free-text instruction appended to the stage's user prompt.
   * Set when the user clicks Resume/Retry with an additional note in the UI.
   */
  userAdditionalPrompt?: string
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
  'concept',
  'backlog',
  'implementation',
  'self_review',
  'finalization',
  'done',
]

export function nextStage(stage: PipelineStage): PipelineStage | null {
  const idx = STAGE_ORDER.indexOf(stage)
  if (idx === -1 || idx === STAGE_ORDER.length - 1)
    return null
  return STAGE_ORDER[idx + 1]
}
