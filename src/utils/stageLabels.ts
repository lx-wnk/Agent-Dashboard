import type { PipelineStage } from '@/types'

export const STAGE_LABELS: Record<PipelineStage, string> = {
  ready: 'Ready',
  plan_review: 'Plan Review',
  backlog: 'Backlog',
  implementation: 'Implementation',
  self_review: 'Self-Review',
  finalization: 'Finalization',
  done: 'Done',
  cancelled: 'Cancelled',
  on_hold: 'On Hold',
}

export const STAGE_DESCRIPTIONS: Record<PipelineStage, string> = {
  ready: 'Task is ready to enter the pipeline',
  plan_review: 'Reviewing the generated execution plan before implementation',
  backlog: 'Exploring ideas and defining the task scope',
  implementation: 'Agent is actively writing code or making changes',
  self_review: 'Agent is reviewing its own output for correctness',
  finalization: 'Wrapping up changes and preparing for completion',
  done: 'Task has been completed successfully',
  cancelled: 'Task was cancelled and will not be processed further',
  on_hold: 'Task is paused awaiting user approval or input',
}
