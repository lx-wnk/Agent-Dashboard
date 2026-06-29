import type { PipelineStage, PipelineTask } from '../types'
import { shallowMount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import PipelineBoard from './PipelineBoard.vue'
import SortableTaskList from './SortableTaskList.vue'

// Mutable state shared between vi.mock factory and test bodies.
// The factory's useTasks() function closes over this variable and reads
// its current value each time it is called (at component mount time),
// so setting it before shallowMount works correctly.
let mockStageMap: Partial<Record<PipelineStage, PipelineTask[]>> = {}

vi.mock('../composables/useTasks', () => ({
  useTasks: () => ({
    tasks: { value: [] as PipelineTask[] },
    tasksByStageMap: {
      // Plain object — not reactive. Tests rely on initial render only.
      get value() { return mockStageMap },
    },
    isLoading: ref(false),
    error: ref(null),
    selectTask: vi.fn(),
    startStream: vi.fn(),
  }),
  reorderTask: vi.fn(),
  byActivityDesc: (a: PipelineTask, b: PipelineTask) =>
    new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime(),
}))

vi.mock('../composables/useProjects', () => ({
  useProjects: () => ({ projects: ref([]) }),
}))

// Prevent SortableTaskList (shallow stub) from calling real useSortable
vi.mock('@vueuse/integrations/useSortable', () => ({
  useSortable: vi.fn(),
}))

beforeEach(() => {
  mockStageMap = {}
})

// COLUMNS order (from PipelineBoard.vue):
// 0 needs-you, 1 concept, 2 backlog, 3 plan_review, 4 implementation,
// 5 finalization, 6 done, 7 cancelled
describe('pipelineBoard — sortable prop threading', () => {
  it('passes sortable=true to needs-you, concept, and backlog SortableTaskLists', () => {
    const wrapper = shallowMount(PipelineBoard)
    const lists = wrapper.findAllComponents(SortableTaskList)
    expect(lists[0].props('sortable')).toBe(true) // needs-you
    expect(lists[1].props('sortable')).toBe(true) // concept
    expect(lists[2].props('sortable')).toBe(true) // backlog
    wrapper.unmount()
  })

  it('passes sortable=false to plan_review, implementation, finalization, done, cancelled', () => {
    const wrapper = shallowMount(PipelineBoard)
    const lists = wrapper.findAllComponents(SortableTaskList)
    expect(lists[3].props('sortable')).toBe(false) // plan_review
    expect(lists[4].props('sortable')).toBe(false) // implementation
    expect(lists[5].props('sortable')).toBe(false) // finalization
    expect(lists[6].props('sortable')).toBe(false) // done
    expect(lists[7].props('sortable')).toBe(false) // cancelled
    wrapper.unmount()
  })
})
