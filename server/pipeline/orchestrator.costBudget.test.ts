import type { PipelineTask } from '../../src/types.js'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import process from 'node:process'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { closeDb, getDb } from '../db/client.js'
import { createStageRun, sumCompletedCostCents, updateStageRun } from '../db/stageRunsRepo.js'
import { createTask } from '../db/tasksRepo.js'

let tmpDir: string

beforeEach(() => {
  tmpDir = mkdtempSync(join(tmpdir(), 'budget-test-'))
  process.env.DASHBOARD_DB_PATH = join(tmpDir, 'test.db')
  getDb()
})

afterEach(() => {
  closeDb()
  delete process.env.DASHBOARD_DB_PATH
  rmSync(tmpDir, { recursive: true, force: true })
})

function makeTaskInput(overrides: Partial<Parameters<typeof createTask>[0]> = {}) {
  return {
    slug: 'budget-test',
    title: 'Budget test task',
    description: null,
    cwd: '/tmp/project',
    costBudgetCents: 500,
    ...overrides,
  }
}

describe('sumCompletedCostCents', () => {
  it('returns 0 when there are no done stage_runs for the task', () => {
    const task = createTask(makeTaskInput())
    expect(sumCompletedCostCents(task.id)).toBe(0)
  })

  it('sums cost_cents of done stage_runs only', () => {
    const task = createTask(makeTaskInput())
    const run1 = createStageRun({ taskId: task.id, stage: 'concept' })
    const run2 = createStageRun({ taskId: task.id, stage: 'implementation' })
    const run3 = createStageRun({ taskId: task.id, stage: 'self_review' })
    updateStageRun(run1.id, { status: 'done', costCents: 120 })
    updateStageRun(run2.id, { status: 'done', costCents: 380 })
    // run3 stays running with cost 200 — should NOT be included
    updateStageRun(run3.id, { costCents: 200 })

    expect(sumCompletedCostCents(task.id)).toBe(500)
  })

  it('excludes failed and running stage_runs from the sum', () => {
    const task = createTask(makeTaskInput())
    const run1 = createStageRun({ taskId: task.id, stage: 'implementation' })
    const run2 = createStageRun({ taskId: task.id, stage: 'self_review' })
    updateStageRun(run1.id, { status: 'failed', costCents: 999 })
    updateStageRun(run2.id, { costCents: 999 }) // stays running

    expect(sumCompletedCostCents(task.id)).toBe(0)
  })

  it('excludes stage_runs belonging to a different task', () => {
    const taskA = createTask(makeTaskInput({ slug: 'task-a' }))
    const taskB = createTask(makeTaskInput({ slug: 'task-b' }))
    const runA = createStageRun({ taskId: taskA.id, stage: 'concept' })
    const runB = createStageRun({ taskId: taskB.id, stage: 'concept' })
    updateStageRun(runA.id, { status: 'done', costCents: 9999 })
    updateStageRun(runB.id, { status: 'done', costCents: 100 })

    expect(sumCompletedCostCents(taskB.id)).toBe(100)
    expect(sumCompletedCostCents(taskA.id)).toBe(9999)
  })
})

describe('orchestrator budget guard condition', () => {
  function shouldEnforceBudget(task: Pick<PipelineTask, 'costBudgetCents'>, spentCents: number): boolean {
    return (
      task.costBudgetCents != null
      && task.costBudgetCents > 0
      && spentCents > task.costBudgetCents
    )
  }

  it('triggers enforcement when spent exceeds budget by 1 cent', () => {
    expect(shouldEnforceBudget({ costBudgetCents: 500 }, 501)).toBe(true)
  })

  it('does not trigger enforcement when spent equals the budget exactly', () => {
    expect(shouldEnforceBudget({ costBudgetCents: 500 }, 500)).toBe(false)
  })

  it('does not trigger enforcement when spent is below the budget', () => {
    expect(shouldEnforceBudget({ costBudgetCents: 500 }, 100)).toBe(false)
  })

  it('does not trigger enforcement when costBudgetCents is null', () => {
    expect(shouldEnforceBudget({ costBudgetCents: null }, 99999)).toBe(false)
  })

  it('does not trigger enforcement when costBudgetCents is 0 (sentinel for disabled)', () => {
    expect(shouldEnforceBudget({ costBudgetCents: 0 }, 99999)).toBe(false)
  })
})
