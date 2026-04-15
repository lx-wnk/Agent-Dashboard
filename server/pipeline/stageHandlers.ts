/**
 * Real stage handlers: every agent-driven stage spawns a detached Claude
 * via agentSpawner and returns an `async_running` transition carrying the
 * PID. The orchestrator's driver loop later finalizes the stage via
 * completionDetector and applies the appropriate next/iterate/wait_user
 * transition.
 *
 * Agent-less stages (backlog, approval1, approval2) produce their
 * transitions inline without spawning — they exist only as bookkeeping
 * gates in the pipeline.
 */
import type { PipelineStage } from '../../src/types.js'
import type { SpawnAgentOptions, SpawnResult } from './agentSpawner.js'
import type { PromptBundle } from './stagePrompts.js'
import type { StageContext, StageHandler, StageTransition } from './types.js'
import { listUnresolvedFeedbackForStage } from '../db/feedbackRepo.js'
import { listStageRunsForTask } from '../db/stageRunsRepo.js'
import { spawnStageAgent } from './agentSpawner.js'
import {
  finalisierungPrompt,
  planningPrompt,
  pruefungPrompt,
  refinementPrompt,
  selbstreviewPrompt,
  umsetzungPrompt,
  umsetzungskonzeptPrompt,
} from './stagePrompts.js'

export type SpawnFn = (opts: SpawnAgentOptions) => SpawnResult
export type PromptBuilder = (ctx: StageContext) => PromptBundle

/**
 * Upper bound on the rejected-output JSON embedded in the retry prompt.
 *  Large umsetzung outputs could otherwise blow the model's context window
 *  on the retry — exactly the failure mode that caused the first rejection.
 *  The error message plus the schema description already convey the shape
 *  expected; the rejected payload is only there for concrete reference.
 */
const REJECTED_OUTPUT_PREVIEW_CHARS = 2000

/**
 * Prepend a correction block to a stage's user prompt when the previous
 * iteration's output was schema-rejected. Keeps the validator's error
 * message front-and-centre so the agent can self-correct.
 */
function buildFeedbackPrefix(priorOutput: Record<string, unknown> | null): string {
  if (!priorOutput || typeof priorOutput.validation_error !== 'string')
    return ''
  const rejected = priorOutput.rejected_output
  let rejectedBlock = ''
  if (rejected) {
    const full = JSON.stringify(rejected, null, 2)
    const truncated = full.length > REJECTED_OUTPUT_PREVIEW_CHARS
      ? `${full.slice(0, REJECTED_OUTPUT_PREVIEW_CHARS)}\n… (truncated, ${full.length - REJECTED_OUTPUT_PREVIEW_CHARS} chars elided)`
      : full
    rejectedBlock = `\n\nYour previous response was:\n\`\`\`json\n${truncated}\n\`\`\``
  }
  return `## CORRECTION REQUIRED\n\nYour previous attempt was rejected with: **${priorOutput.validation_error}**.${rejectedBlock}\n\nStick EXACTLY to the schema described below. Do not add or rename fields.\n\n---\n\n`
}

/**
 * Generic factory for an agent-driven stage. Given a stage id and a
 * prompt builder that reads from the StageContext, produces a handler
 * that spawns, records audit, and returns async_running.
 *
 * The spawn function is injected so tests can assert the spawn call shape
 * without actually launching a real Claude process.
 */
export function createAgentStage(
  stage: PipelineStage,
  buildPrompt: PromptBuilder,
  spawn: SpawnFn = spawnStageAgent,
): StageHandler {
  return {
    stage,
    requiresAgent: true,
    async execute(ctx: StageContext): Promise<StageTransition> {
      const bundle = buildPrompt(ctx)
      const feedback = buildFeedbackPrefix(ctx.priorIterationOutput)
      const result = spawn({
        task: ctx.task,
        stageRun: ctx.stageRun,
        systemPrompt: bundle.systemPrompt,
        prompt: feedback + bundle.userPrompt,
        permissions: ctx.permissions,
      })
      ctx.recordAudit(`${stage}_spawned`, {
        pid: result.pid,
        iteration: ctx.stageRun.iteration,
        hasFeedback: feedback.length > 0,
      })
      return { kind: 'async_running', pid: result.pid }
    },
  }
}

// ───── Prompt adapters — bridge `stagePrompts.ts` to the PromptBuilder shape.
// Each adapter reads what it needs from the context (task, previousOutput,
// task metadata for the umsetzung feedback loop) and delegates to the
// corresponding builder in stagePrompts.ts.

const pruefungBuilder: PromptBuilder = ctx => pruefungPrompt(ctx.task)

const refinementBuilder: PromptBuilder = ctx => refinementPrompt(ctx.task, ctx.previousOutput)

const planningBuilder: PromptBuilder = (ctx) => {
  const userFeedback = listUnresolvedFeedbackForStage(ctx.task.id, 'planning')
  return planningPrompt(ctx.task, ctx.previousOutput, userFeedback)
}

const umsetzungskonzeptBuilder: PromptBuilder = (ctx) => {
  const userFeedback = listUnresolvedFeedbackForStage(ctx.task.id, 'umsetzungskonzept')
  return umsetzungskonzeptPrompt(ctx.task, ctx.previousOutput, userFeedback)
}

/**
 * Umsetzung reads optional `review_feedback` from task.metadata — the
 * orchestrator writes it there when a selbstreview iteration rejects the
 * prior implementation. First-run tasks see an empty feedback string.
 */
const umsetzungBuilder: PromptBuilder = (ctx) => {
  const feedback = typeof ctx.task.metadata?.review_feedback === 'string'
    ? ctx.task.metadata.review_feedback as string
    : undefined
  return umsetzungPrompt(ctx.task, ctx.previousOutput, feedback)
}

const selbstreviewBuilder: PromptBuilder = ctx =>
  selbstreviewPrompt(ctx.task, ctx.previousOutput)

const finalisierungBuilder: PromptBuilder = ctx =>
  finalisierungPrompt(ctx.task, listStageRunsForTask(ctx.task.id))

// ───── Agent-less stages: handlers that transition synchronously without
// spawning a process. These are the pipeline's plumbing gates.

export const backlogHandler: StageHandler = {
  stage: 'backlog',
  requiresAgent: false,
  async execute(ctx: StageContext): Promise<StageTransition> {
    ctx.recordAudit('backlog_entered')
    return { kind: 'next', toStage: 'pruefung' }
  },
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

// ───── Real agent stages.

export const pruefungHandler = createAgentStage('pruefung', pruefungBuilder)
export const refinementHandler = createAgentStage('refinement', refinementBuilder)
export const planningHandler = createAgentStage('planning', planningBuilder)
export const approval1Handler = approvalStage('approval1', 'Bitte Plan freigeben')
export const umsetzungskonzeptHandler = createAgentStage('umsetzungskonzept', umsetzungskonzeptBuilder)
export const approval2Handler = approvalStage(
  'approval2',
  'Bitte Umsetzungskonzept + Tool-Permissions freigeben',
)
export const umsetzungHandler = createAgentStage('umsetzung', umsetzungBuilder)
export const selbstreviewHandler = createAgentStage('selbstreview', selbstreviewBuilder)
export const finalisierungHandler = createAgentStage('finalisierung', finalisierungBuilder)

// ───── Backwards-compatible named factory for the pruefung handler.
// Tests that predate the generic factory import this directly.
export function createPruefungHandler(spawn: SpawnFn = spawnStageAgent): StageHandler {
  return createAgentStage('pruefung', pruefungBuilder, spawn)
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
