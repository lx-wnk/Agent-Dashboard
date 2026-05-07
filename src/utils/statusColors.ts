import type { PipelineStage, StageRunStatus } from '../types'

/**
 * Tailwind classes for a pipeline-stage chip (bg + text + border).
 * Consumers must add `border` to their base class.
 */
export function stageChipClass(stage: PipelineStage | string): string {
  switch (stage) {
    case 'on_hold':
    case 'approval1':
    case 'approval2':
      return 'bg-yellow-50 dark:bg-yellow-950/50 text-yellow-600 dark:text-yellow-400 border-yellow-200 dark:border-yellow-800/60'
    case 'implementation':
      return 'bg-blue-50 dark:bg-blue-950/50 text-blue-600 dark:text-blue-400 border-blue-300 dark:border-blue-700'
    case 'done':
      return 'bg-green-50 dark:bg-green-950/50 text-green-600 dark:text-green-400 border-green-300 dark:border-green-700'
    case 'cancelled':
      return 'bg-red-50 dark:bg-red-950/50 text-red-600 dark:text-red-400 border-red-300 dark:border-red-700'
    default:
      return 'bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 border-slate-200 dark:border-slate-700'
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
      return 'bg-blue-50 dark:bg-blue-950/50 text-blue-600 dark:text-blue-400 border-blue-300 dark:border-blue-600/50'
    case 'done':
      return 'bg-green-50 dark:bg-green-950/50 text-green-600 dark:text-green-400 border-green-200 dark:border-green-700/50'
    case 'failed':
      return 'bg-red-50 dark:bg-red-950/50 text-red-600 dark:text-red-400 border-red-200 dark:border-red-700/50'
    case 'on_hold':
    case 'awaiting_user':
      return 'bg-yellow-50 dark:bg-yellow-950/50 text-yellow-600 dark:text-yellow-400 border-yellow-200 dark:border-yellow-700/50'
    default:
      return 'bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-600 border-slate-200 dark:border-slate-700'
  }
}

/**
 * Tailwind classes for an agent-session status chip (bg + text, no border).
 * Used in TaskModal session-status badge.
 */
export function agentSessionStatusClass(status: string): string {
  switch (status) {
    case 'active':
      return 'bg-green-50 dark:bg-green-950/50 text-green-600 dark:text-green-400'
    case 'waiting':
      return 'bg-yellow-50 dark:bg-yellow-950/50 text-yellow-600 dark:text-yellow-400'
    default:
      return 'bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-600'
  }
}
