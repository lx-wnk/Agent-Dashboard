import type { PipelineTask } from '../types'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import TaskCard from './TaskCard.vue'

const baseTask: PipelineTask = {
  id: 'task-1',
  slug: 'my-task',
  title: 'My Task Title',
  description: null,
  cwd: '/tmp',
  worktreePath: null,
  sourceBranch: null,
  targetBranch: null,
  currentStage: 'backlog',
  parentTaskId: null,
  maxIterations: 3,
  tokenBudget: null,
  costBudgetCents: null,
  stageTimeoutSeconds: 1800,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
  metadata: null,
  silverBullet: false,
  priority: 'medium',
  userId: null,
}

const stubs = {
  WorktreePill: true,
}

describe('TaskCard', () => {
  it('renders a real button with data-testid="task-card-open"', () => {
    const wrapper = mount(TaskCard, {
      props: { task: baseTask },
      global: { stubs },
    })
    expect(wrapper.find('button[data-testid="task-card-open"]').exists()).toBe(true)
  })

  it('aria-label on the open button contains the task title', () => {
    const wrapper = mount(TaskCard, {
      props: { task: baseTask },
      global: { stubs },
    })
    const btn = wrapper.find('button[data-testid="task-card-open"]')
    expect(btn.attributes('aria-label')).toContain('My Task Title')
  })

  it('clicking the open button emits select with the task', async () => {
    const wrapper = mount(TaskCard, {
      props: { task: baseTask },
      global: { stubs },
    })
    await wrapper.find('button[data-testid="task-card-open"]').trigger('click')
    expect(wrapper.emitted('select')).toBeTruthy()
    expect(wrapper.emitted('select')![0]).toEqual([baseTask])
  })

  it('shows Continue Chat button when currentStage is concept', () => {
    const task = { ...baseTask, currentStage: 'concept' as const }
    const wrapper = mount(TaskCard, {
      props: { task },
      global: { stubs },
    })
    expect(wrapper.find('button[data-testid="task-card-open"]').exists()).toBe(true)
    const buttons = wrapper.findAll('button')
    const continueChat = buttons.find(b => b.text().includes('Continue Chat'))
    expect(continueChat).toBeTruthy()
  })

  it('Continue Chat emits openChat and NOT select (click.stop)', async () => {
    const task = { ...baseTask, currentStage: 'concept' as const }
    const wrapper = mount(TaskCard, {
      props: { task },
      global: { stubs },
    })
    const buttons = wrapper.findAll('button')
    const continueChat = buttons.find(b => b.text().includes('Continue Chat'))!
    await continueChat.trigger('click')
    expect(wrapper.emitted('openChat')).toBeTruthy()
    expect(wrapper.emitted('openChat')![0]).toEqual([task])
    expect(wrapper.emitted('select')).toBeFalsy()
  })
})
