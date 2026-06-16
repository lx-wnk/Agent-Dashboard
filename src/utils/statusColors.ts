import type { PipelineStage, StageRunStatus } from '../types'

/**
 * Maps an agent/badge status string to a human-readable display label.
 * Single source of truth for status → label mapping (SSOT, UX-016).
 */
export function statusLabel(status: string): string {
  switch (status) {
    case 'active': return 'Active'
    case 'waiting': return 'Waiting'
    case 'idle': return 'Idle'
    case 'completed': return 'Completed'
    case 'error': return 'Error'
    case 'info': return 'Info'
    default: return status
  }
}

/**
 * Tailwind classes for a pipeline-stage chip (bg + text + border).
 * Consumers must add `border` to their base class.
 */
export function stageChipClass(stage: PipelineStage | string): string {
  switch (stage) {
    case 'on_hold':
      return 'bg-warning-soft text-warning-text border-warning-line'
    case 'implementation':
      return 'bg-info-soft text-info-text border-info-line'
    case 'done':
      return 'bg-success-soft text-success-text border-success-line'
    case 'cancelled':
      return 'bg-danger-soft text-danger-text border-danger-line'
    default:
      return 'bg-neutral-soft text-neutral-text border-neutral-line'
  }
}

/**
 * Tailwind classes for a stage-run status chip (bg + text + border).
 * Consumers must add `border` to their base class.
 * Used in TaskCard run-status badge and TaskModal stage rows.
 */
export function runStatusChipClass(status: StageRunStatus | string): string {
  switch (status) {
    case 'running':
      return 'bg-info-soft text-info-text border-info-line'
    case 'done':
      return 'bg-success-soft text-success-text border-success-line'
    case 'failed':
      return 'bg-danger-soft text-danger-text border-danger-line'
    case 'on_hold':
    case 'awaiting_user':
      return 'bg-warning-soft text-warning-text border-warning-line'
    default:
      return 'bg-neutral-soft text-neutral-text border-neutral-line'
  }
}

/**
 * Tailwind classes for an agent-session status chip (bg + text, no border).
 * Used in TaskModal session-status badge.
 */
export function agentSessionStatusClass(status: string): string {
  switch (status) {
    case 'active':
      return 'bg-success-soft text-success-text'
    case 'waiting':
      return 'bg-warning-soft text-warning-text'
    case 'idle':
      return 'bg-neutral-soft text-neutral-text'
    case 'completed':
      return 'bg-success-soft text-success-text'
    case 'error':
      return 'bg-danger-soft text-danger-text'
    default:
      return 'bg-neutral-soft text-neutral-text'
  }
}
