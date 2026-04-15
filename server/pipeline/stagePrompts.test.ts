import type { PipelineTask, StageRun } from '../../src/types.js'
import { describe, expect, it } from 'vitest'
import {
  finalisierungPrompt,
  planningPrompt,
  pruefungPrompt,
  selbstreviewPrompt,
  umsetzungPrompt,
  umsetzungskonzeptPrompt,
} from './stagePrompts.js'

function makeTask(overrides: Partial<PipelineTask> = {}): PipelineTask {
  return {
    id: 't-1',
    slug: 'fix-login',
    title: 'Fix login bug',
    description: 'Users can no longer log in',
    cwd: '/tmp/project',
    worktreePath: null,
    sourceBranch: 'main',
    targetBranch: null,
    currentStage: 'pruefung',
    parentTaskId: null,
    maxIterations: 20,
    tokenBudget: null,
    costBudgetCents: null,
    stageTimeoutSeconds: 1800,
    createdAt: '2026-04-12T10:00:00Z',
    updatedAt: '2026-04-12T10:00:00Z',
    metadata: null,
    silverBullet: false,
    priority: 'medium',
    ...overrides,
  }
}

describe('stagePrompts', () => {
  it('pruefungPrompt embeds task title, description, and requests a JSON block', () => {
    const { systemPrompt, userPrompt } = pruefungPrompt(makeTask())
    expect(systemPrompt).toContain('structured task pipeline')
    expect(userPrompt).toContain('Fix login bug')
    expect(userPrompt).toContain('Users can no longer log in')
    expect(userPrompt).toContain('```json')
    expect(userPrompt).toContain('recommendation')
  })

  it('planningPrompt embeds previous stage output', () => {
    const prev = { findings: ['missing validation'], complexity: 'M' }
    const { userPrompt } = planningPrompt(makeTask(), prev)
    expect(userPrompt).toContain('"findings"')
    expect(userPrompt).toContain('missing validation')
    expect(userPrompt).toContain('acceptanceCriteria')
  })

  it('umsetzungskonzeptPrompt requests tool-permission inventory', () => {
    const { userPrompt } = umsetzungskonzeptPrompt(makeTask(), { subtasks: [] })
    expect(userPrompt).toContain('toolRequests')
    expect(userPrompt).toContain('Bash(git push *)')
    expect(userPrompt).toContain('pattern')
  })

  it('umsetzungPrompt has an orchestrator system prompt and omits feedback when absent', () => {
    const { systemPrompt, userPrompt } = umsetzungPrompt(makeTask(), { steps: [] })
    expect(systemPrompt).toContain('Opus orchestrator')
    expect(systemPrompt).toContain('Task tool')
    expect(userPrompt).not.toContain('Review Feedback')
  })

  it('umsetzungPrompt includes feedback when provided for iteration', () => {
    const { userPrompt } = umsetzungPrompt(
      makeTask(),
      { steps: [] },
      'missing tests for auth middleware',
    )
    expect(userPrompt).toContain('Review Feedback From Previous Iteration')
    expect(userPrompt).toContain('missing tests for auth middleware')
  })

  it('selbstreviewPrompt requests a findings array with severities', () => {
    const { userPrompt } = selbstreviewPrompt(makeTask(), { diff: 'patch' })
    expect(userPrompt).toContain('findings')
    expect(userPrompt).toContain('severity')
    expect(userPrompt).toContain('passed')
  })

  it('finalisierungPrompt renders the stage-run history', () => {
    const runs: StageRun[] = [
      {
        id: 'r1',
        taskId: 't-1',
        stage: 'umsetzung',
        sessionId: null,
        sessionName: null,
        pid: null,
        status: 'done',
        startedAt: null,
        endedAt: null,
        iteration: 0,
        output: null,
        tokensUsed: 0,
        costCents: 0,
      },
      {
        id: 'r2',
        taskId: 't-1',
        stage: 'selbstreview',
        sessionId: null,
        sessionName: null,
        pid: null,
        status: 'done',
        startedAt: null,
        endedAt: null,
        iteration: 0,
        output: null,
        tokensUsed: 0,
        costCents: 0,
      },
    ]
    const { userPrompt } = finalisierungPrompt(makeTask(), runs)
    expect(userPrompt).toContain('umsetzung (iter 0): done')
    expect(userPrompt).toContain('selbstreview (iter 0): done')
    expect(userPrompt).toContain('testPlan')
  })
})
