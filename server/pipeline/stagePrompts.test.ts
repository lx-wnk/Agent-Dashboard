import type { PipelineTask, StageRun } from '../../src/types.js'
import { describe, expect, it } from 'vitest'
import {
  finalizationPrompt,
  implementationPrompt,
  selfReviewPrompt,
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
    currentStage: 'implementation',
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
    userId: null,
    ...overrides,
  }
}

describe('stagePrompts', () => {
  it('implementationPrompt has an orchestrator system prompt and omits feedback when absent', () => {
    const { systemPrompt, userPrompt } = implementationPrompt(makeTask(), { steps: [] })
    expect(systemPrompt).toContain('Opus orchestrator')
    expect(systemPrompt).toContain('Task tool')
    expect(userPrompt).not.toContain('Review Feedback')
  })

  it('implementationPrompt includes feedback when provided for iteration', () => {
    const { userPrompt } = implementationPrompt(
      makeTask(),
      { steps: [] },
      'missing tests for auth middleware',
    )
    expect(userPrompt).toContain('Review Feedback From Previous Iteration')
    expect(userPrompt).toContain('missing tests for auth middleware')
  })

  it('implementationPrompt embeds the concept-stage output as the plan', () => {
    const concept = { spec: 'rewrite auth', steps: [{ n: 1, description: 'rm old' }] }
    const { userPrompt } = implementationPrompt(makeTask(), concept)
    expect(userPrompt).toContain('Concept')
    expect(userPrompt).toContain('rewrite auth')
    expect(userPrompt).toContain('rm old')
  })

  it('selfReviewPrompt requests a findings array with severities', () => {
    const { userPrompt } = selfReviewPrompt(makeTask(), { diff: 'patch' })
    expect(userPrompt).toContain('findings')
    expect(userPrompt).toContain('severity')
    expect(userPrompt).toContain('passed')
  })

  it('finalizationPrompt renders the stage-run history', () => {
    const runs: StageRun[] = [
      {
        id: 'r1',
        taskId: 't-1',
        stage: 'implementation',
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
        stage: 'self_review',
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
    const { userPrompt } = finalizationPrompt(makeTask(), runs)
    expect(userPrompt).toContain('implementation (iter 0): done')
    expect(userPrompt).toContain('self_review (iter 0): done')
    expect(userPrompt).toContain('testPlan')
  })
})
