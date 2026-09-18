import type { PermissionItem } from '@/composables/usePendingPermissions'
import type { PermissionRequest, PipelineTask } from '@/types'
import { describe, expect, it } from 'vitest'
import { KIND_RANK, rankNextThings } from '../composables/useNextThing'

function task(id: string, over: Partial<PipelineTask> = {}): PipelineTask {
  return { id, slug: id, title: `task ${id}`, currentStage: 'implementation', cwd: '/repo', ...over } as PipelineTask
}

function req(id: string, at: string, over: Partial<PermissionRequest> = {}): PermissionRequest {
  return { id, stageRunId: 'run', tool: 'Bash', pattern: 'task lint', requestedAt: at, resolvedAt: null, outcome: null, ...over } as PermissionRequest
}

function item(taskId: string, requests: PermissionRequest[]): PermissionItem {
  return { taskId, title: `task ${taskId}`, projectName: 'Dashboard', routineId: null, requests }
}

describe('rankNextThings', () => {
  // The whole point of the centre: a stopped agent outranks a decision that
  // nothing is waiting on.
  it('puts a blocked permission ahead of a plan waiting for approval', () => {
    const out = rankNextThings(
      [item('a', [req('r1', '2026-09-18T12:00:00Z')])],
      [task('a'), task('b', { currentStage: 'plan_review' })],
    )
    expect(out.map(n => n.kind)).toEqual(['permission', 'plan'])
    expect(out[0].taskId).toBe('a')
  })

  // Without this a trickle of new requests starves the oldest one.
  it('serves the longest wait first within one kind', () => {
    const out = rankNextThings(
      [item('a', [req('new', '2026-09-18T12:00:00Z')]), item('b', [req('old', '2026-09-18T09:00:00Z')])],
      [task('a'), task('b')],
    )
    expect(out.map(n => n.request?.id)).toEqual(['old', 'new'])
  })

  it('names the request by tool and pattern, which is the headline', () => {
    const out = rankNextThings([item('a', [req('r1', '2026-09-18T12:00:00Z')])], [task('a')])
    expect(out[0].title).toBe('Bash(task lint)')
    expect(out[0].why).toContain('stopped until you answer')
  })

  it('is empty when nothing is blocked and nothing waits for approval', () => {
    expect(rankNextThings([], [task('a'), task('b')])).toHaveLength(0)
  })

  // A kind that forgets its place sorts as undefined and lands anywhere.
  it('gives every kind a distinct place in the order', () => {
    const ranks = Object.values(KIND_RANK)
    expect(new Set(ranks).size).toBe(ranks.length)
  })
})
