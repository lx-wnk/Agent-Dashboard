/**
 * Real stage handlers: every agent-driven stage spawns a detached Claude
 * via agentSpawner and returns an `async_running` transition carrying the
 * PID. The orchestrator's driver loop later finalizes the stage via
 * completionDetector and applies the appropriate next/iterate/wait_user
 * transition.
 *
 * Agent-less stages (konzept, backlog) produce their transitions inline
 * without spawning. `konzept` is handled by an interactive chat flow
 * outside the orchestrator — the handler below is a safety net only.
 * `backlog` is the "Ready for Doing" gate that transitions immediately
 * into `umsetzung`.
 */
import type { PipelineStage } from '../../src/types.js'
import type { SpawnAgentOptions, SpawnResult } from './agentSpawner.js'
import type { PromptBundle } from './stagePrompts.js'
import type { StageContext, StageHandler, StageTransition } from './types.js'
import process from 'node:process'
import { generateApiToken, hashApiToken, upsertStageRunApiKey } from '../db/apiKeysRepo.js'
import { listStageRunsForTask } from '../db/stageRunsRepo.js'
import { spawnStageAgent } from './agentSpawner.js'
import {
  finalisierungPrompt,
  selbstreviewPrompt,
  umsetzungPrompt,
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

      // Generate a stage-run-scoped MCP token; revoked in orchestrator.applyTransition
      // on every terminal transition. Lifetime is state-transition-bounded, not time-bounded.
      const rawToken = generateApiToken()
      const keyHash = hashApiToken(rawToken)
      void upsertStageRunApiKey({
        name: `stage-run:${ctx.stageRun.id}`,
        keyHash,
        scopes: ['tasks:read'],
      })

      const port = process.env.DASHBOARD_PORT || '13120'
      const result = spawn({
        task: ctx.task,
        stageRun: ctx.stageRun,
        systemPrompt: bundle.systemPrompt,
        prompt: feedback + bundle.userPrompt + (ctx.userAdditionalPrompt ? `\n\n---\nAdditional instruction from user: ${ctx.userAdditionalPrompt}` : ''),
        permissions: ctx.permissions,
        resumeSessionId: ctx.resumeSessionId ?? null,
        mcpToken: rawToken,
        mcpUrl: `http://127.0.0.1:${port}/api/mcp`,
      })
      ctx.recordAudit(`${stage}_spawned`, {
        pid: result.pid,
        iteration: ctx.stageRun.iteration,
        hasFeedback: feedback.length > 0,
        resumedSessionId: ctx.resumeSessionId ?? null,
      })
      return { kind: 'async_running', pid: result.pid }
    },
  }
}

// ───── Prompt adapters — bridge `stagePrompts.ts` to the PromptBuilder shape.
// Each adapter reads what it needs from the context (task, previousOutput,
// task metadata for the umsetzung feedback loop) and delegates to the
// corresponding builder in stagePrompts.ts.

/**
 * Implementation stage reads optional `review_feedback` from task.metadata — the
 * orchestrator writes it there when a selbstreview iteration rejects the
 * prior implementation. First-run tasks see an empty feedback string.
 *
 * Concept-stage output (spec, plan, toolRequests) is stored in task.metadata by
 * `POST /api/refine/:taskId/confirm` when the user confirms the refinement
 * chat — there is no longer a prior stage_run whose output we read.
 */
const umsetzungBuilder: PromptBuilder = (ctx) => {
  const feedback = typeof ctx.task.metadata?.review_feedback === 'string'
    ? ctx.task.metadata.review_feedback as string
    : undefined
  const konzeptOutput = (ctx.task.metadata ?? {}) as Record<string, unknown>
  return umsetzungPrompt(ctx.task, konzeptOutput, feedback)
}

const selbstreviewBuilder: PromptBuilder = ctx =>
  selbstreviewPrompt(ctx.task, ctx.previousOutput)

const finalisierungBuilder: PromptBuilder = ctx =>
  finalisierungPrompt(ctx.task, listStageRunsForTask(ctx.task.id))

// ───── Agent-less stages: handlers that transition synchronously without
// spawning a process. These are the pipeline's plumbing gates.

/**
 * konzeptHandler — agent-less safety net; orchestrator never picks up
 * konzept tasks in the normal path (they are excluded from
 * `listPickableTasks`). The interactive refinement chat runs outside the
 * state machine; when the user confirms, the task is advanced to
 * `backlog` by the refine-confirm route. This handler exists only so the
 * handler map stays exhaustive.
 */
export const konzeptHandler: StageHandler = {
  stage: 'konzept',
  requiresAgent: false,
  async execute(ctx: StageContext): Promise<StageTransition> {
    ctx.recordAudit('konzept_chat_pending')
    return { kind: 'wait_user', reason: 'Refinement chat in progress' }
  },
}

export const backlogHandler: StageHandler = {
  stage: 'backlog',
  requiresAgent: false,
  async execute(ctx: StageContext): Promise<StageTransition> {
    ctx.recordAudit('backlog_entered')
    return { kind: 'next', toStage: 'umsetzung' }
  },
}

// ───── Real agent stages.

export const umsetzungHandler = createAgentStage('umsetzung', umsetzungBuilder)
export const selbstreviewHandler = createAgentStage('selbstreview', selbstreviewBuilder)
export const finalisierungHandler = createAgentStage('finalisierung', finalisierungBuilder)

export const handlersByStage: Record<string, StageHandler> = {
  konzept: konzeptHandler,
  backlog: backlogHandler,
  umsetzung: umsetzungHandler,
  selbstreview: selbstreviewHandler,
  finalisierung: finalisierungHandler,
}

export function getHandlerForStage(stage: PipelineStage): StageHandler | null {
  return handlersByStage[stage] ?? null
}
