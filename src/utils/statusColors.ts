import type { PipelineStage, StageRunStatus } from '../types'

export type ChipTone = 'success' | 'warning' | 'danger' | 'info' | 'neutral' | 'accent'

export function stageTone(stage: PipelineStage | string): ChipTone {
  switch (stage) {
    case 'done': return 'success'
    case 'cancelled': return 'danger'
    case 'on_hold': return 'warning'
    case 'implementation': return 'info'
    default: return 'neutral'
  }
}

export const RUN_STATUS_LABELS: Record<StageRunStatus, string> = {
  pending: 'Pending',
  running: 'Running',
  awaiting_user: 'Waiting',
  on_hold: 'On Hold',
  done: 'Done',
  failed: 'Failed',
  requeued: 'Requeued',
}

export function runStatusLabel(status: StageRunStatus): string {
  return RUN_STATUS_LABELS[status] ?? status
}

export function runStatusTone(status: StageRunStatus | string): ChipTone {
  switch (status) {
    case 'running': return 'info'
    case 'done': return 'success'
    case 'failed': return 'danger'
    case 'on_hold':
    case 'awaiting_user': return 'warning'
    default: return 'neutral'
  }
}

export function agentStatusTone(status: string): ChipTone {
  switch (status) {
    case 'active': return 'success'
    case 'waiting': return 'warning'
    case 'error': return 'danger'
    case 'completed': return 'success'
    default: return 'neutral'
  }
}

/** Maps an agent/badge status string to a human-readable display label (SSOT, UX-016). */
export function statusLabel(status: string): string {
  switch (status) {
    case 'active': return 'Active'
    case 'waiting': return 'Waiting'
    case 'idle': return 'Idle'
    case 'finished': return 'Finished'
    case 'completed': return 'Completed'
    case 'error': return 'Error'
    case 'info': return 'Info'
    default: return status
  }
}
