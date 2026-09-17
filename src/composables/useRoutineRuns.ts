import { readErrorMessage } from '@/utils/errorMessage'

export interface RoutineRun {
  taskId: string
  title: string
  kind: 'pipeline' | 'job'
  stage: string
  status: string
  summary: string
  costCents: number
  startedAt?: string
  endedAt?: string
}

export async function fetchRoutineRuns(scheduleId: string): Promise<RoutineRun[]> {
  const res = await fetch(`/api/schedules/${scheduleId}/runs`)
  if (!res.ok)
    throw new Error(await readErrorMessage(res, `HTTP ${res.status}`))
  return res.json()
}
