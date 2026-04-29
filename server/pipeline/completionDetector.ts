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
import type { StageOutputRead } from './sessionOutputReader.js'
import { attachSessionId, isPidAlive } from './sessionManager.js'
import { findNewestSessionId, readLastStageJsonOutput, resolvedProjectDir } from './sessionOutputReader.js'

const AGENT_MESSAGE_MAX_CHARS = 2000

export interface CompletionResult {
  kind: 'still_running' | 'completed' | 'failed'
  /**
   * Present on 'completed' always, and on 'failed' either (a) when the
   * agent's JSON payload parsed but the schema was rejected (retryable)
   * or (b) when the agent wrote prose but no JSON block (not retryable,
   * but the prose is surfaced in `output.agentMessage` for the UI).
   */
  output?: Record<string, unknown>
  /** Present on 'failed' — human-readable reason. */
  error?: string
  /**
   * True when the failure is a schema rejection of a parsed JSON payload —
   * the orchestrator can meaningfully retry with a `validation_error`
   * feedback loop. False (or absent) for hard failures like missing
   * session, missing JSON block, or prose-only agent output: retrying
   * would just re-run into the same wall.
   */
  retryable?: boolean
}

export interface DetectCompletionDeps {
  isPidAlive?: (pid: number | null) => boolean
  readOutput?: (cwd: string, sessionId: string) => Promise<StageOutputRead>
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
    case 'selbstreview':
      return validateSelbstreview(output)
    case 'finalisierung':
      return validateFinalisierung(output)
    default:
      // Stages without a structured schema (backlog, umsetzung) just
      // need to be a parseable object. No further checks.
      return { ok: true }
  }
}

function missing(field: string): ValidationResult {
  return { ok: false, error: `missing required field: ${field}` }
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

  if (!sessionId) {
    const searchedDir = await resolvedProjectDir(cwd)
    return {
      kind: 'failed',
      error: `no session JSONL found in ${searchedDir} after ${stageRun.startedAt} (cwd=${cwd})`,
    }
  }

  const { output, rawText } = await read(cwd, sessionId)
  if (!output) {
    // Hard fail. If the agent wrote prose (typical: permission wall,
    // refusal, or plain forgot the ```json block), surface the tail so
    // the user sees in the modal what the agent actually said, instead
    // of the mechanical error-only payload.
    if (rawText) {
      const trimmed = rawText.length > AGENT_MESSAGE_MAX_CHARS
        ? `${rawText.slice(-AGENT_MESSAGE_MAX_CHARS)}`
        : rawText
      return {
        kind: 'failed',
        error: 'agent did not produce a ```json output block',
        output: { agentMessage: trimmed },
      }
    }
    return { kind: 'failed', error: 'no parseable json output in session tail' }
  }

  const validation = validate(stageRun.stage, output)
  if (!validation.ok) {
    return {
      kind: 'failed',
      error: validation.error ?? 'output validation failed',
      output,
      retryable: true,
    }
  }

  return { kind: 'completed', output }
}
