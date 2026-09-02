import type { PipelineTask, StageRun } from '../types'

export function formatCents(cents: number): string {
  if (cents === 0)
    return '—'
  if (cents < 100)
    return `${cents}¢`
  return `$${(cents / 100).toFixed(2)}`
}

export function formatDuration(ms: number): string {
  const h = Math.floor(ms / 3_600_000)
  const m = Math.floor((ms % 3_600_000) / 60_000)
  const s = Math.floor((ms % 60_000) / 1_000)
  if (h > 0)
    return `${h}h ${m}m`
  if (m > 0)
    return `${m}m ${s}s`
  return `${s}s`
}

// Wall-clock age: task creation → completion (or now, while still running).
export function taskRuntime(task: PipelineTask): string {
  const start = new Date(task.createdAt).getTime()
  const end = task.currentStage === 'done' || task.currentStage === 'cancelled'
    ? new Date(task.updatedAt).getTime()
    : Date.now()
  return formatDuration(end - start)
}

// Active runtime: summed stage-run execution time, excluding queue/idle gaps.
export function activeRuntime(stageRuns: StageRun[]): string {
  const ms = stageRuns.reduce((sum, r) => {
    if (!r.startedAt)
      return sum
    const start = new Date(r.startedAt).getTime()
    const end = r.endedAt ? new Date(r.endedAt).getTime() : Date.now()
    return sum + Math.max(0, end - start)
  }, 0)
  return ms === 0 ? '—' : formatDuration(ms)
}
