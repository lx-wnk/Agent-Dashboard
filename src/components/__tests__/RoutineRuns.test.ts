import type { PipelineTask } from '@/types'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { shallowRef } from 'vue'
import RoutineRuns from '@/components/RoutineRuns.vue'
import { toast } from '@/composables/useToast'

const taskStore = shallowRef<PipelineTask[]>([])
const selectTask = vi.fn()

vi.mock('@/features/pipeline/composables/useTasks', () => ({
  useTasks: () => ({ tasks: taskStore, selectTask }),
}))

const SCHEDULE_ID = 'sched-1'

function makeTask(overrides: Partial<PipelineTask> = {}): PipelineTask {
  return {
    id: 'task-1',
    slug: 'task-1',
    title: 'Task 1',
    description: null,
    cwd: '/home/user',
    worktreePath: null,
    sourceBranch: null,
    targetBranch: null,
    currentStage: 'backlog',
    parentTaskId: null,
    maxIterations: 1,
    tokenBudget: null,
    routineId: SCHEDULE_ID,
    latestStageRunStatus: null,
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
    ...overrides,
  } as any
}

function makeRun(overrides: Partial<{
  taskId: string
  title: string
  kind: string
  stage: string
  status: string
  summary: string
  costCents: number
  startedAt: string
  endedAt: string
}> = {}) {
  return {
    taskId: 'task-1',
    title: 'Task 1',
    kind: 'job',
    stage: 'done',
    status: 'completed',
    summary: 'It worked',
    costCents: 12,
    ...overrides,
  }
}

let runsResponse: { status: number, body: unknown }
let fetchMock: ReturnType<typeof vi.fn>
let runsCallCount: number

beforeEach(() => {
  taskStore.value = []
  selectTask.mockClear()
  runsResponse = { status: 200, body: [] }
  runsCallCount = 0
  fetchMock = vi.fn(async (url: string) => {
    if (url === `/api/schedules/${SCHEDULE_ID}/runs`) {
      runsCallCount++
      return {
        ok: runsResponse.status < 400,
        status: runsResponse.status,
        json: async () => runsResponse.body,
      }
    }
    if (url === '/api/tasks/task-2') {
      return {
        ok: true,
        status: 200,
        json: async () => makeTask({ id: 'task-2' }),
      }
    }
    throw new Error(`unexpected fetch ${url}`)
  })
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('routineRuns', () => {
  it('renders runs with status, summary, cost, and duration', async () => {
    runsResponse.body = [
      makeRun({ taskId: 'task-1', costCents: 12, startedAt: '2026-01-01T00:00:00Z', endedAt: '2026-01-01T00:02:05Z' }),
      makeRun({ taskId: 'task-2', title: 'Task 2', status: 'failed', summary: 'It broke' }),
    ]
    const wrapper = mount(RoutineRuns, { props: { scheduleId: SCHEDULE_ID } })
    await flushPromises()

    const row1 = wrapper.get('[data-testid="routine-run-task-1"]')
    expect(row1.text()).toContain('completed')
    expect(row1.text()).toContain('It worked')
    expect(row1.text()).toContain('$0.12')
    expect(row1.text()).toContain('2m 5s')

    const row2 = wrapper.get('[data-testid="routine-run-task-2"]')
    expect(row2.text()).toContain('failed')
    expect(row2.text()).toContain('It broke')

    wrapper.unmount()
  })

  it('shows "No runs yet" for an empty list', async () => {
    runsResponse.body = []
    const wrapper = mount(RoutineRuns, { props: { scheduleId: SCHEDULE_ID } })
    await flushPromises()

    expect(wrapper.text()).toContain('No runs yet')

    wrapper.unmount()
  })

  it('shows the server error message on a 404 and not the empty state', async () => {
    runsResponse = { status: 404, body: { error: 'not found' } }
    const wrapper = mount(RoutineRuns, { props: { scheduleId: SCHEDULE_ID } })
    await flushPromises()

    const alert = wrapper.get('[role="alert"]')
    expect(alert.text()).toBe('not found')
    expect(wrapper.text()).not.toContain('No runs yet')

    wrapper.unmount()
  })

  it('opens a run whose task is already in the store without fetching it', async () => {
    runsResponse.body = [makeRun({ taskId: 'task-1' })]
    taskStore.value = [makeTask({ id: 'task-1' })]
    const wrapper = mount(RoutineRuns, { props: { scheduleId: SCHEDULE_ID } })
    await flushPromises()

    await wrapper.get('[data-testid="routine-run-task-1"] button').trigger('click')
    await flushPromises()

    expect(selectTask).toHaveBeenCalledWith(taskStore.value[0])
    expect(fetchMock).not.toHaveBeenCalledWith('/api/tasks/task-1', expect.anything())
    expect(fetchMock.mock.calls.some(([url]) => url === '/api/tasks/task-1')).toBe(false)

    wrapper.unmount()
  })

  it('fetches and opens a run whose task is not in the store', async () => {
    runsResponse.body = [makeRun({ taskId: 'task-2' })]
    const wrapper = mount(RoutineRuns, { props: { scheduleId: SCHEDULE_ID } })
    await flushPromises()

    await wrapper.get('[data-testid="routine-run-task-2"] button').trigger('click')
    await flushPromises()

    expect(fetchMock.mock.calls.some(([url]) => url === '/api/tasks/task-2')).toBe(true)
    expect(selectTask).toHaveBeenCalledWith(expect.objectContaining({ id: 'task-2' }))

    wrapper.unmount()
  })

  it('re-fetches when a task of this routine changes stage, but not for another routine', async () => {
    taskStore.value = [makeTask({ id: 'task-1', currentStage: 'backlog' })]
    const wrapper = mount(RoutineRuns, { props: { scheduleId: SCHEDULE_ID } })
    await flushPromises()
    expect(runsCallCount).toBe(1)

    taskStore.value = [makeTask({ id: 'task-99', routineId: 'other-schedule', currentStage: 'backlog' }), ...taskStore.value]
    await flushPromises()
    expect(runsCallCount).toBe(1)

    taskStore.value = [makeTask({ id: 'task-1', currentStage: 'ready' })]
    await flushPromises()
    expect(runsCallCount).toBe(2)

    wrapper.unmount()
  })

  it('keeps the previous run list visible during a live reload instead of showing the loading state', async () => {
    runsResponse.body = [makeRun({ taskId: 'task-1' })]
    taskStore.value = [makeTask({ id: 'task-1', currentStage: 'backlog' })]
    const wrapper = mount(RoutineRuns, { props: { scheduleId: SCHEDULE_ID } })
    await flushPromises()
    expect(wrapper.find('[data-testid="routine-run-task-1"]').exists()).toBe(true)

    let resolveSecond: (value: unknown) => void = () => {}
    const pending = new Promise((resolve) => {
      resolveSecond = resolve
    })
    fetchMock.mockImplementationOnce(async (url: string) => {
      if (url === `/api/schedules/${SCHEDULE_ID}/runs`) {
        await pending
        return { ok: true, status: 200, json: async () => runsResponse.body }
      }
      throw new Error(`unexpected fetch ${url}`)
    })

    taskStore.value = [makeTask({ id: 'task-1', currentStage: 'ready' })]
    await flushPromises()

    expect(wrapper.text()).not.toContain('Loading runs…')
    expect(wrapper.find('[data-testid="routine-run-task-1"]').exists()).toBe(true)

    resolveSecond(undefined)
    await flushPromises()

    wrapper.unmount()
  })

  it('shows an error toast when opening a run whose task fetch fails', async () => {
    const errorSpy = vi.spyOn(toast, 'error').mockImplementation(() => '')
    runsResponse.body = [makeRun({ taskId: 'task-2' })]
    fetchMock.mockImplementation(async (url: string) => {
      if (url === `/api/schedules/${SCHEDULE_ID}/runs`)
        return { ok: true, status: 200, json: async () => runsResponse.body }
      if (url === '/api/tasks/task-2')
        return { ok: false, status: 500, json: async () => ({}) }
      throw new Error(`unexpected fetch ${url}`)
    })
    const wrapper = mount(RoutineRuns, { props: { scheduleId: SCHEDULE_ID } })
    await flushPromises()

    await wrapper.get('[data-testid="routine-run-task-2"] button').trigger('click')
    await flushPromises()

    expect(errorSpy).toHaveBeenCalledWith(expect.stringContaining('500'))

    errorSpy.mockRestore()
    wrapper.unmount()
  })
})
