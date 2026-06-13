import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import TaskCard from '../TaskCard.vue'

const FULL_UUID = '812f85f4-aaaa-bbbb-cccc-dddddddddddd'

function makeTask(overrides = {}) {
  return {
    id: FULL_UUID,
    slug: 'my-task',
    title: 'Test Task',
    description: null,
    cwd: '/home/user',
    worktreePath: null,
    sourceBranch: null,
    targetBranch: null,
    currentStage: 'backlog',
    parentTaskId: null,
    maxIterations: 10,
    tokenBudget: null,
    costBudgetCents: null,
    stageTimeoutSeconds: 3600,
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
    metadata: null,
    silverBullet: false,
    priority: 'medium',
    userId: null,
    ...overrides,
  } as any
}

vi.mock('../WorktreePill.vue', () => ({ default: { template: '<span />' } }))

vi.mock('@vueuse/core', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@vueuse/core')>()
  return {
    ...actual,
    useClipboard: () => ({
      copy: vi.fn().mockResolvedValue(undefined),
      copied: ref(false),
    }),
  }
})

describe('taskCard — short-id chip', () => {
  let writeTextMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    writeTextMock = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText: writeTextMock },
      configurable: true,
    })
  })

  it('renders the first 8 chars of task.id in the chip', () => {
    const wrapper = mount(TaskCard, { props: { task: makeTask() } })
    expect(wrapper.text()).toContain('#812f85f4')
  })

  it('does NOT trigger the select emit when the copy button is clicked', async () => {
    const wrapper = mount(TaskCard, { props: { task: makeTask() } })
    const btn = wrapper.find('button[aria-label^="Copy task id"]')
    expect(btn.exists()).toBe(true)
    await btn.trigger('click')
    expect(wrapper.emitted('select')).toBeFalsy()
  })

  it('copy button has aria-label containing full UUID and title = full UUID', () => {
    const wrapper = mount(TaskCard, { props: { task: makeTask() } })
    const btn = wrapper.find('button[aria-label^="Copy task id"]')
    expect(btn.attributes('aria-label')).toContain(FULL_UUID)
    expect(btn.attributes('title')).toBe(FULL_UUID)
  })
})
