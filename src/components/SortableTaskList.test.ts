import type { PipelineTask, Project } from '../types'
import { shallowMount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import SortableTaskList from './SortableTaskList.vue'
import TaskCard from './TaskCard.vue'

// Capture useSortable callbacks so tests can trigger them manually.
let capturedOnStart: (() => void) | undefined
let capturedOnEnd: ((evt: { oldIndex?: number, newIndex?: number }) => void) | undefined
let capturedOptions: Record<string, unknown> = {}

vi.mock('@vueuse/integrations/useSortable', () => ({
  useSortable: (_el: unknown, _list: unknown, opts: Record<string, unknown>) => {
    capturedOnStart = opts?.onStart as () => void
    capturedOnEnd = opts?.onEnd as (evt: { oldIndex?: number, newIndex?: number }) => void
    capturedOptions = { ...opts }
    return undefined
  },
}))

// SortableTaskList only imports reorderTask — mock the whole module to avoid SSE side-effects.
vi.mock('../composables/useTasks', () => ({
  reorderTask: vi.fn(),
}))

beforeEach(() => {
  capturedOnStart = undefined
  capturedOnEnd = undefined
  capturedOptions = {}
})

function makeTask(id: string, overrides: Partial<PipelineTask> = {}): PipelineTask {
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
    updatedAt: '2026-01-01T00:00:00Z',
    metadata: null,
    silverBullet: false,
    planMode: false,
    priority: 'medium',
    userId: null,
    rank: null,
    ...overrides,
  }
}

const emptyProjectById = new Map<string, Project>()

describe('sortableTaskList — drag guard', () => {
  it('resets list normally when a prop update arrives outside a drag', async () => {
    const taskA = makeTask('a')
    const taskB = makeTask('b')
    const wrapper = shallowMount(SortableTaskList, {
      props: { tasks: [taskA, taskB], projectById: emptyProjectById },
    })

    await wrapper.setProps({ tasks: [taskB, taskA] })
    await nextTick()

    const cards = wrapper.findAllComponents(TaskCard)
    expect(cards[0].props('task').id).toBe('b')
    expect(cards[1].props('task').id).toBe('a')

    wrapper.unmount()
  })

  it('does NOT reset list while a drag is in progress (onStart fired, onEnd not yet)', async () => {
    const taskA = makeTask('a')
    const taskB = makeTask('b')
    const wrapper = shallowMount(SortableTaskList, {
      props: { tasks: [taskA, taskB], projectById: emptyProjectById },
    })

    capturedOnStart?.()

    // SSE delivers new prop order during the drag
    await wrapper.setProps({ tasks: [taskB, taskA] })
    await nextTick()

    // Guard must have fired — list should still show original order
    const cards = wrapper.findAllComponents(TaskCard)
    expect(cards[0].props('task').id).toBe('a')
    expect(cards[1].props('task').id).toBe('b')

    wrapper.unmount()
  })

  it('resets list after drag ends (onEnd clears isDragging before returning)', async () => {
    const taskA = makeTask('a')
    const taskB = makeTask('b')
    const wrapper = shallowMount(SortableTaskList, {
      props: { tasks: [taskA, taskB], projectById: emptyProjectById },
    })

    capturedOnStart?.()
    // End with same position — still clears isDragging
    capturedOnEnd?.({ oldIndex: 0, newIndex: 0 })

    await wrapper.setProps({ tasks: [taskB, taskA] })
    await nextTick()

    const cards = wrapper.findAllComponents(TaskCard)
    expect(cards[0].props('task').id).toBe('b')
    expect(cards[1].props('task').id).toBe('a')

    wrapper.unmount()
  })

  it('skips reset when incoming order matches current (same IDs, same positions)', async () => {
    const taskA = makeTask('a')
    const taskB = makeTask('b')
    const wrapper = shallowMount(SortableTaskList, {
      props: { tasks: [taskA, taskB], projectById: emptyProjectById },
    })

    // New objects, same IDs, same order — simulates an SSE field-update with no rerank
    await wrapper.setProps({ tasks: [{ ...taskA, updatedAt: '2026-06-01T00:00:00Z' }, { ...taskB }] })
    await nextTick()

    const cards = wrapper.findAllComponents(TaskCard)
    expect(cards[0].props('task').id).toBe('a')
    expect(cards[1].props('task').id).toBe('b')

    wrapper.unmount()
  })
})

describe('sortableTaskList — sortable prop', () => {
  it('passes disabled:true to useSortable when sortable is false', () => {
    shallowMount(SortableTaskList, {
      props: { tasks: [], projectById: emptyProjectById, sortable: false },
    }).unmount()
    expect(capturedOptions.disabled).toBe(true)
  })

  it('passes disabled:false to useSortable when sortable is true (default)', () => {
    shallowMount(SortableTaskList, {
      props: { tasks: [], projectById: emptyProjectById },
    }).unmount()
    expect(capturedOptions.disabled).toBe(false)
  })

  it('forwards sortable prop to each TaskCard', () => {
    const wrapper = shallowMount(SortableTaskList, {
      props: { tasks: [makeTask('x')], projectById: emptyProjectById, sortable: false },
    })
    const card = wrapper.findAllComponents(TaskCard)[0]
    expect(card.props('sortable')).toBe(false)
    wrapper.unmount()
  })
})
