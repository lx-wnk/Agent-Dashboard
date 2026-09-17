import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick, ref } from 'vue'

vi.mock('@/features/pipeline/composables/useTasks', () => ({
  useTasks: () => ({ tasks: ref([]), selectTask: vi.fn() }),
}))

let SchedulesView: any
let toastMod: typeof import('../../composables/useToast')

beforeEach(async () => {
  vi.resetModules()
  toastMod = await import('../../composables/useToast')
  vi.spyOn(toastMod.toast, 'error')
  globalThis.fetch = vi.fn().mockResolvedValue({ ok: false, status: 500, json: async () => ({}) })
  class FakeEventSource {
    static readonly CLOSED = 2
    readyState = 0
    close = vi.fn()
  }
  globalThis.EventSource = FakeEventSource as any
  SchedulesView = (await import('../SchedulesView.vue')).default
})

describe('schedulesView', () => {
  it('calls toast.error and renders no inline danger text when load fails', async () => {
    const w = mount(SchedulesView)
    await flushPromises()
    await nextTick()
    expect(toastMod.toast.error).toHaveBeenCalled()
    expect(w.find('.text-danger-text').exists()).toBe(false)
  })
})

const SCHEDULE_ID = 'sched-1'

function makeSchedule(overrides: Partial<Record<string, unknown>> = {}) {
  return {
    id: SCHEDULE_ID,
    name: 'Nightly build',
    enabled: true,
    nlText: null,
    cronExpr: '0 2 * * *',
    human: 'At 02:00 every day',
    timezone: 'UTC',
    catchup: 'none',
    runMode: 'job',
    slugPrefix: 'nightly',
    title: 'Nightly build',
    description: null,
    cwd: '/home/user/project',
    priority: 'medium',
    silverBullet: false,
    maxIterations: 1,
    projectId: null,
    spawnerId: null,
    permissionTemplate: null,
    nextRunAt: '2026-01-02T02:00:00Z',
    lastRunAt: '2026-01-01T02:00:00Z',
    lastTaskId: null,
    lastSkippedAt: null,
    skippedCount: 0,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    applications: [],
    ...overrides,
  }
}

function makeFetchMock(schedule: Record<string, unknown>) {
  return vi.fn(async (url: string) => {
    if (url === '/api/schedules')
      return { ok: true, status: 200, json: async () => [schedule] }
    if (url === `/api/schedules/${schedule.id}/runs`)
      return { ok: true, status: 200, json: async () => [] }
    return { ok: true, status: 200, json: async () => [] }
  })
}

describe('schedulesView — run mode, skips, and runs', () => {
  it('shows Job for a job runMode and Pipeline task for a pipeline runMode', async () => {
    globalThis.fetch = makeFetchMock(makeSchedule({ runMode: 'job' })) as any
    const w = mount(SchedulesView)
    await flushPromises()
    expect(w.get(`[data-testid="schedule-run-mode-${SCHEDULE_ID}"]`).text()).toBe('Job')
    w.unmount()

    globalThis.fetch = makeFetchMock(makeSchedule({ runMode: 'pipeline' })) as any
    const w2 = mount(SchedulesView)
    await flushPromises()
    expect(w2.get(`[data-testid="schedule-run-mode-${SCHEDULE_ID}"]`).text()).toBe('Pipeline task')
    w2.unmount()
  })

  it('shows a skipped-runs note when skippedCount is greater than zero, and nothing when zero', async () => {
    globalThis.fetch = makeFetchMock(makeSchedule({ skippedCount: 3, lastSkippedAt: '2026-01-01T03:00:00Z' })) as any
    const w = mount(SchedulesView)
    await flushPromises()
    const note = w.get(`[data-testid="schedule-skipped-${SCHEDULE_ID}"]`)
    expect(note.text()).toContain('Skipped 3 times')
    w.unmount()

    globalThis.fetch = makeFetchMock(makeSchedule({ skippedCount: 0 })) as any
    const w2 = mount(SchedulesView)
    await flushPromises()
    expect(w2.find(`[data-testid="schedule-skipped-${SCHEDULE_ID}"]`).exists()).toBe(false)
    w2.unmount()
  })

  it('renders the runs block only after Runs is clicked, and fetches runs then', async () => {
    const fetchMock = makeFetchMock(makeSchedule())
    globalThis.fetch = fetchMock as any
    const w = mount(SchedulesView)
    await flushPromises()

    expect(w.find(`#schedule-runs-${SCHEDULE_ID}`).exists()).toBe(false)

    const toggle = w.get(`[aria-controls="schedule-runs-${SCHEDULE_ID}"]`)
    expect(toggle.attributes('aria-expanded')).toBe('false')
    await toggle.trigger('click')
    await flushPromises()

    expect(toggle.attributes('aria-expanded')).toBe('true')
    expect(w.find(`#schedule-runs-${SCHEDULE_ID}`).exists()).toBe(true)
    expect(fetchMock.mock.calls.some(([url]) => url === `/api/schedules/${SCHEDULE_ID}/runs`)).toBe(true)

    w.unmount()
  })
})
