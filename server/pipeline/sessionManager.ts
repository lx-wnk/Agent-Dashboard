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
 *
 * EPERM handling: process.kill(pid, 0) throws EPERM when the PID exists but
 * is owned by a different user. We treat this as "alive" to avoid wrongly
 * restarting a foreign process on top of it. A PID-reuse race (same PID, now
 * a foreign process) would then be misclassified as alive — the recovery
 * path gates further on session_id existence to recover safely.
 */
export function isPidAlive(pid: number | null): boolean {
  if (pid === null || pid <= 0)
    return false
  try {
    process.kill(pid, 0)
    return true
  }
  catch (err) {
    if ((err as NodeJS.ErrnoException).code === 'EPERM')
      return true
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
