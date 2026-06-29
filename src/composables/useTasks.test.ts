import type { PipelineTask } from '../types'
import { describe, expect, it } from 'vitest'
import { byActivityDesc } from './useTasks'

function makeTask(id: string, updatedAt: string): PipelineTask {
  return {
    id,
    slug: `slug-${id}`,
    title: `Task ${id}`,
    description: null,
    cwd: '/repo',
    worktreePath: null,
    sourceBranch: null,
    targetBranch: null,
    currentStage: 'backlog',
    parentTaskId: null,
    maxIterations: 10,
    tokenBudget: null,
    costBudgetCents: null,
    stageTimeoutSeconds: 300,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt,
    metadata: null,
    silverBullet: false,
    planMode: false,
    priority: 'medium',
    userId: null,
    rank: null,
  }
}

describe('byActivityDesc', () => {
  it('places the task with the newer updatedAt first', () => {
    const older = makeTask('a', '2026-01-01T00:00:00Z')
    const newer = makeTask('b', '2026-06-01T00:00:00Z')
    expect([older, newer].sort(byActivityDesc).map(t => t.id)).toEqual(['b', 'a'])
  })

  it('returns 0 for equal timestamps', () => {
    const ts = '2026-06-01T12:00:00Z'
    expect(byActivityDesc(makeTask('x', ts), makeTask('y', ts))).toBe(0)
  })

  it('sorts three tasks newest-first', () => {
    const tasks = [
      makeTask('mid', '2026-03-01T00:00:00Z'),
      makeTask('newest', '2026-06-01T00:00:00Z'),
      makeTask('oldest', '2026-01-01T00:00:00Z'),
    ]
    expect([...tasks].sort(byActivityDesc).map(t => t.id)).toEqual(['newest', 'mid', 'oldest'])
  })
})
