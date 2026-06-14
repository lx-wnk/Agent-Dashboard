import type { PipelineTask } from '../types'

export function formatTaskDate(iso: string | null): string {
  if (!iso)
    return '—'
  return new Date(iso).toLocaleString()
}

export function formatCents(cents: number): string {
  if (cents === 0)
    return '—'
  if (cents < 100)
    return `${cents}¢`
  return `$${(cents / 100).toFixed(2)}`
}

export function taskRuntime(task: PipelineTask): string {
  const start = new Date(task.createdAt).getTime()
  const end = task.currentStage === 'done' || task.currentStage === 'cancelled'
    ? new Date(task.updatedAt).getTime()
    : Date.now()
  const ms = end - start
  const h = Math.floor(ms / 3_600_000)
  const m = Math.floor((ms % 3_600_000) / 60_000)
  const s = Math.floor((ms % 60_000) / 1_000)
  if (h > 0)
    return `${h}h ${m}m`
  if (m > 0)
    return `${m}m ${s}s`
  return `${s}s`
}
