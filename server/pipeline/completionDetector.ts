/**
 * Determines whether an async-running stage_run has finished, and if so,
 * whether its output conforms to the stage's expected schema. Used by the
 * orchestrator's tick loop to convert a dead PID into a next-stage,
 * retry-with-feedback, or await-user decision.
 *
 * Validation strategy (user-chosen, see memory: feedback_llm_output_validation):
 * strict per-stage schema check at this layer. The validator returns pass/fail
 * only; the retry-vs-escalate decision lives in the orchestrator which
 * inspects `stageRun.iteration` to choose between `iterate` and `wait_user`.
 */
import type { PipelineStage, StageRun } from '../../src/types.js'
import { attachSessionId, isPidAlive } from './sessionManager.js'
import { findNewestSessionId, readLastStageJsonOutput } from './sessionOutputReader.js'

export interface CompletionResult {
  kind: 'still_running' | 'completed' | 'failed'
  /**
   * Present on 'completed' and on 'failed' when the failure is a schema
   *  rejection rather than a spawn/exit failure — distinguishes validation
   *  drift (retryable) from hard crashes (not retryable).
   */
  output?: Record<string, unknown>
  /** Present on 'failed' — human-readable reason. */
  error?: string
}

export interface DetectCompletionDeps {
  isPidAlive?: (pid: number | null) => boolean
  readOutput?: (cwd: string, sessionId: string) => Promise<Record<string, unknown> | null>
  findSessionId?: (cwd: string, afterIso: string | null) => Promise<string | null>
  persistSessionId?: (stageRunId: string, sessionId: string) => void
  validate?: (stage: PipelineStage, output: Record<string, unknown>) => ValidationResult
}

export interface ValidationResult {
  ok: boolean
  error?: string
}

/**
 * Strict per-stage schema validation. Expected keys are anchored to the
 * JSON contract documented in server/pipeline/stagePrompts.ts — keep the
 * two files in sync when prompts evolve.
 *
 * Missing/wrong-type keys produce a failure with a specific error string
 * so the orchestrator can feed it back to the agent on the retry prompt.
 */
export function validateStageOutput(
  stage: PipelineStage,
  output: Record<string, unknown>,
): ValidationResult {
  switch (stage) {
    case 'pruefung':
      return validatePruefung(output)
    case 'refinement':
      return validateRefinement(output)
    case 'planning':
      return validatePlanning(output)
    case 'umsetzungskonzept':
      return validateUmsetzungskonzept(output)
    case 'selbstreview':
      return validateSelbstreview(output)
    case 'finalisierung':
      return validateFinalisierung(output)
    default:
      // Stages without a structured schema (backlog, approvals, refinement,
      // umsetzung) just need to be a parseable object. No further checks.
      return { ok: true }
  }
}

function missing(field: string): ValidationResult {
  return { ok: false, error: `missing required field: ${field}` }
}
function wrongType(field: string, expected: string): ValidationResult {
  return { ok: false, error: `field ${field} must be ${expected}` }
}

function validatePruefung(o: Record<string, unknown>): ValidationResult {
  if (typeof o.wellDefined !== 'boolean')
    return missing('wellDefined (boolean)')
  if (!Array.isArray(o.risks))
    return missing('risks (string array)')
  if (typeof o.complexity !== 'string' || !['XS', 'S', 'M', 'L', 'XL'].includes(o.complexity))
    return wrongType('complexity', 'one of XS|S|M|L|XL')
  if (!Array.isArray(o.blockers))
    return missing('blockers (string array)')
  if (typeof o.recommendation !== 'string' || !['proceed', 'refine', 'reject'].includes(o.recommendation))
    return wrongType('recommendation', 'one of proceed|refine|reject')
  return { ok: true }
}

function validateRefinement(o: Record<string, unknown>): ValidationResult {
  if (typeof o.refinedTitle !== 'string')
    return missing('refinedTitle (string)')
  if (typeof o.refinedDescription !== 'string')
    return missing('refinedDescription (string)')
  if (!Array.isArray(o.successCriteria))
    return missing('successCriteria (string array)')
  return { ok: true }
}

function validatePlanning(o: Record<string, unknown>): ValidationResult {
  if (!Array.isArray(o.subtasks))
    return missing('subtasks (array)')
  if (!Array.isArray(o.acceptanceCriteria))
    return missing('acceptanceCriteria (string array)')
  return { ok: true }
}

function validateUmsetzungskonzept(o: Record<string, unknown>): ValidationResult {
  if (!Array.isArray(o.steps))
    return missing('steps (array)')
  if (!Array.isArray(o.toolRequests))
    return missing('toolRequests (array)')
  return { ok: true }
}

function validateSelbstreview(o: Record<string, unknown>): ValidationResult {
  if (typeof o.passed !== 'boolean')
    return missing('passed (boolean)')
  if (!Array.isArray(o.findings))
    return missing('findings (array)')
  if (typeof o.summary !== 'string')
    return missing('summary (string)')
  return { ok: true }
}

function validateFinalisierung(o: Record<string, unknown>): ValidationResult {
  if (typeof o.summary !== 'string')
    return missing('summary (string)')
  if (!Array.isArray(o.insights))
    return missing('insights (string array)')
  if (!Array.isArray(o.openTodos))
    return missing('openTodos (string array)')
  if (!Array.isArray(o.testPlan))
    return missing('testPlan (string array)')
  return { ok: true }
}

/**
 * Inspect a stage_run and decide whether it has completed, failed, or is
 * still running. Pure in its injected deps — tests swap out the PID probe,
 * session lookup, and JSONL reader to avoid spawning real processes.
 *
 * When `kind === 'failed'` and `output` IS present, the failure is a
 * schema-rejection of a parsed payload — the orchestrator should treat
 * this as retryable (iterate with feedback). When `output` is absent,
 * the agent never produced a parseable result and the failure is hard.
 */
export async function detectCompletion(
  stageRun: StageRun,
  cwd: string,
  deps: DetectCompletionDeps = {},
): Promise<CompletionResult> {
  const probe = deps.isPidAlive ?? isPidAlive
  const read = deps.readOutput ?? readLastStageJsonOutput
  const findSession = deps.findSessionId ?? findNewestSessionId
  const persist = deps.persistSessionId ?? attachSessionId
  const validate = deps.validate ?? validateStageOutput

  if (probe(stageRun.pid))
    return { kind: 'still_running' }

  let sessionId = stageRun.sessionId
  if (!sessionId) {
    // Refuse to scan for a session when startedAt is null — without a
    // time anchor we'd match any pre-existing .jsonl in the cwd, which
    // could belong to an unrelated user session. A recovered 'pending'
    // stage_run that never started has no business completing here.
    if (!stageRun.startedAt)
      return { kind: 'failed', error: 'stage_run never started — cannot locate session' }
    sessionId = await findSession(cwd, stageRun.startedAt)
    if (sessionId)
      persist(stageRun.id, sessionId)
  }

  if (!sessionId)
    return { kind: 'failed', error: 'agent exited without producing a session file' }

  const output = await read(cwd, sessionId)
  if (!output)
    return { kind: 'failed', error: 'no parseable json output in session tail' }

  const validation = validate(stageRun.stage, output)
  if (!validation.ok)
    return { kind: 'failed', error: validation.error ?? 'output validation failed', output }

  return { kind: 'completed', output }
}
