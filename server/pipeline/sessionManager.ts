import type { PipelineStage, PipelineTask, StageRun } from '../../src/types.js'
import { execFileSync } from 'node:child_process'
import process from 'node:process'
import { findStageRunBySessionId, updateStageRun } from '../db/stageRunsRepo.js'
import { IS_LINUX } from '../platform.js'

const ZOMBIE_STATE_RE = /^State:\s+Z/m

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
function isPidZombie(pid: number): boolean {
  try {
    if (IS_LINUX) {
      const stat = execFileSync('cat', [`/proc/${pid}/status`], { encoding: 'utf8', timeout: 500 })
      return ZOMBIE_STATE_RE.test(stat)
    }
    else {
      // macOS: ps -p PID -o stat= returns the state character(s); 'Z' = zombie
      const stat = execFileSync('ps', ['-p', String(pid), '-o', 'stat='], { encoding: 'utf8', timeout: 500 }).trim()
      return stat.startsWith('Z')
    }
  }
  catch {
    return false
  }
}

export function isPidAlive(pid: number | null): boolean {
  if (pid === null || pid <= 0)
    return false
  try {
    process.kill(pid, 0)
    // kill(0) succeeds for zombie processes too — verify the process isn't defunct
    return !isPidZombie(pid)
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
  if (existing) {
    // Already attached (same run = no-op; different run = don't steal it).
    return
  }
  updateStageRun(stageRunId, { sessionId })
}
