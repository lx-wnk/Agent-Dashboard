import type { PipelineStage, PipelineTask, StageRun } from '../../src/types.js'
import process from 'node:process'
import { findStageRunBySessionId, updateStageRun } from '../db/stageRunsRepo.js'

/**
 * Build a human-readable session name from task + stage + iteration.
 * Format: {slug}-{stage}-iter-{n}  (e.g. fix-login-bug-umsetzung-iter-3)
 */
export function buildSessionName(task: PipelineTask, stage: PipelineStage, iteration: number): string {
  return `${task.slug}-${stage}-iter-${iteration}`
}

/**
 * Check whether a given PID is still alive (without sending a real signal).
 * Returns false if the process has exited or PID is null.
 */
export function isPidAlive(pid: number | null): boolean {
  if (pid === null || pid <= 0)
    return false
  try {
    process.kill(pid, 0)
    return true
  }
  catch {
    return false
  }
}

/**
 * Decide whether a running stage_run can be reconnected (live PID) or must
 * be resumed (dead PID but session_id still exists on disk).
 */
export interface RecoveryDecision {
  kind: 'alive' | 'resume' | 'restart'
  reason: string
}

export function decideRecovery(stageRun: StageRun): RecoveryDecision {
  if (isPidAlive(stageRun.pid))
    return { kind: 'alive', reason: `PID ${stageRun.pid} still running` }
  if (stageRun.sessionId)
    return { kind: 'resume', reason: `session ${stageRun.sessionId} available for --resume` }
  return { kind: 'restart', reason: 'no live PID and no session — must start fresh' }
}

/**
 * Persist a session id onto a stage run when it becomes known. Safe no-op
 * if already set to the same value.
 */
export function attachSessionId(stageRunId: string, sessionId: string): void {
  const existing = findStageRunBySessionId(sessionId)
  if (existing && existing.id === stageRunId)
    return
  updateStageRun(stageRunId, { sessionId })
}
