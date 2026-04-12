import type { PipelineStage } from '../../src/types.js'
import type { StageContext, StageHandler, StageTransition } from './types.js'

/**
 * Phase 2 note: Stage handlers are stubbed here so the orchestrator and
 * state-machine logic can be developed and tested without real agents.
 * Phase 4 will replace each body with a concrete spawn/analyse/record flow.
 */

function simpleAgentStage(stage: PipelineStage): StageHandler {
  return {
    stage,
    requiresAgent: true,
    async execute(ctx: StageContext): Promise<StageTransition> {
      ctx.recordAudit(`${stage}_started`)
      return { kind: 'next', toStage: autoNext(stage), output: { stub: stage } }
    },
  }
}

function approvalStage(stage: PipelineStage, reason: string): StageHandler {
  return {
    stage,
    requiresAgent: false,
    async execute(ctx: StageContext): Promise<StageTransition> {
      ctx.recordAudit(`${stage}_awaiting_user`, { reason })
      return { kind: 'wait_user', reason }
    },
  }
}

function autoNext(stage: PipelineStage): PipelineStage {
  const order: PipelineStage[] = [
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
  const idx = order.indexOf(stage)
  return idx >= 0 && idx < order.length - 1 ? order[idx + 1] : 'done'
}

export const backlogHandler: StageHandler = {
  stage: 'backlog',
  requiresAgent: false,
  async execute(ctx: StageContext): Promise<StageTransition> {
    ctx.recordAudit('backlog_entered')
    return { kind: 'next', toStage: 'pruefung' }
  },
}

export const pruefungHandler = simpleAgentStage('pruefung')
export const refinementHandler = simpleAgentStage('refinement')
export const planningHandler = simpleAgentStage('planning')
export const approval1Handler = approvalStage('approval1', 'Bitte Plan freigeben')
export const umsetzungskonzeptHandler = simpleAgentStage('umsetzungskonzept')
export const approval2Handler = approvalStage(
  'approval2',
  'Bitte Umsetzungskonzept + Tool-Permissions freigeben',
)

// Umsetzung has an iterate-until-review-passes loop in Phase 4
export const umsetzungHandler: StageHandler = {
  stage: 'umsetzung',
  requiresAgent: true,
  async execute(ctx: StageContext): Promise<StageTransition> {
    ctx.recordAudit('umsetzung_started')
    return { kind: 'next', toStage: 'selbstreview', output: { stub: 'umsetzung' } }
  },
}

export const selbstreviewHandler: StageHandler = {
  stage: 'selbstreview',
  requiresAgent: true,
  async execute(ctx: StageContext): Promise<StageTransition> {
    ctx.recordAudit('selbstreview_started')
    // Phase 2 stub: always pass
    return { kind: 'next', toStage: 'finalisierung', output: { passed: true } }
  },
}

export const finalisierungHandler: StageHandler = {
  stage: 'finalisierung',
  requiresAgent: true,
  async execute(ctx: StageContext): Promise<StageTransition> {
    ctx.recordAudit('finalisierung_started')
    return { kind: 'done', output: { summary: 'Task completed (Phase 2 stub)' } }
  },
}

export const handlersByStage: Record<string, StageHandler> = {
  backlog: backlogHandler,
  pruefung: pruefungHandler,
  refinement: refinementHandler,
  planning: planningHandler,
  approval1: approval1Handler,
  umsetzungskonzept: umsetzungskonzeptHandler,
  approval2: approval2Handler,
  umsetzung: umsetzungHandler,
  selbstreview: selbstreviewHandler,
  finalisierung: finalisierungHandler,
}

export function getHandlerForStage(stage: PipelineStage): StageHandler | null {
  return handlersByStage[stage] ?? null
}
