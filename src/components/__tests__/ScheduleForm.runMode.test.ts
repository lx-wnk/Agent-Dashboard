import type { ScheduleView } from '@/composables/useSchedules'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ScheduleForm from '@/components/ScheduleForm.vue'

afterEach(() => {
  vi.unstubAllGlobals()
})

function stubFetch(scheduleResponse: unknown, calls: Array<{ url: string, init?: RequestInit }>) {
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    calls.push({ url, init })
    if (url === '/api/applications')
      return { ok: true, status: 200, json: async () => [] }
    return { ok: true, status: 200, json: async () => scheduleResponse }
  }))
}

const existingPipelineSchedule: ScheduleView = {
  id: 's1',
  name: 'Nightly',
  enabled: true,
  cronExpr: '0 0 * * *',
  human: 'every day at midnight',
  timezone: 'UTC',
  catchup: 'once',
  runMode: 'pipeline',
  skippedCount: 0,
  slugPrefix: 'sched-nightly',
  title: 'Nightly run',
  cwd: '/repo',
  priority: 'medium',
  silverBullet: false,
  maxIterations: 20,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
  applications: [],
}

describe('scheduleForm — run mode', () => {
  it('defaults a new routine to Job', async () => {
    const calls: Array<{ url: string, init?: RequestInit }> = []
    stubFetch({ id: 's1' }, calls)

    const wrapper = mount(ScheduleForm)
    await flushPromises()

    expect((wrapper.find('[data-testid="schedule-run-mode-job"]').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.find('[data-testid="schedule-run-mode-pipeline"]').element as HTMLInputElement).checked).toBe(false)
    wrapper.unmount()
  })

  it('sends runMode: pipeline when Pipeline task is chosen', async () => {
    const calls: Array<{ url: string, init?: RequestInit }> = []
    stubFetch({ id: 's1' }, calls)

    const wrapper = mount(ScheduleForm)
    await flushPromises()
    await wrapper.find('[data-testid="schedule-run-mode-pipeline"]').setValue(true)
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    const post = calls.find(c => c.url === '/api/schedules' && c.init?.method === 'POST')
    expect(JSON.parse(String(post?.init?.body)).runMode).toBe('pipeline')
    wrapper.unmount()
  })

  it('shows Pipeline task checked when editing a pipeline schedule', async () => {
    const calls: Array<{ url: string, init?: RequestInit }> = []
    stubFetch(existingPipelineSchedule, calls)

    const wrapper = mount(ScheduleForm, { props: { schedule: existingPipelineSchedule } })
    await flushPromises()

    expect((wrapper.find('[data-testid="schedule-run-mode-pipeline"]').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.find('[data-testid="schedule-run-mode-job"]').element as HTMLInputElement).checked).toBe(false)
    wrapper.unmount()
  })

  it('explains the git-repository requirement for Pipeline task', async () => {
    const calls: Array<{ url: string, init?: RequestInit }> = []
    stubFetch({ id: 's1' }, calls)

    const wrapper = mount(ScheduleForm)
    await flushPromises()

    expect(wrapper.text()).toContain('The working directory must be a git repository.')
    wrapper.unmount()
  })

  it('shows the server error when cwd is not a git repository', async () => {
    const calls: Array<{ url: string, init?: RequestInit }> = []
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      calls.push({ url, init })
      if (url === '/api/applications')
        return { ok: true, status: 200, json: async () => [] }
      return { ok: false, status: 400, json: async () => ({ error: 'working directory is not a git repository' }) }
    }))

    const wrapper = mount(ScheduleForm)
    await flushPromises()
    await wrapper.find('[data-testid="schedule-run-mode-pipeline"]').setValue(true)
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(wrapper.find('[role="alert"]').text()).toBe('working directory is not a git repository')
    wrapper.unmount()
  })

  it('sends catchup as a string and round-trips an existing "once" schedule', async () => {
    const calls: Array<{ url: string, init?: RequestInit }> = []
    stubFetch({ id: 's1' }, calls)

    const wrapper = mount(ScheduleForm)
    await flushPromises()
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    const post = calls.find(c => c.url === '/api/schedules' && c.init?.method === 'POST')
    expect(JSON.parse(String(post?.init?.body)).catchup).toBe('none')
    wrapper.unmount()

    const editCalls: Array<{ url: string, init?: RequestInit }> = []
    stubFetch(existingPipelineSchedule, editCalls)
    const editWrapper = mount(ScheduleForm, { props: { schedule: existingPipelineSchedule } })
    await flushPromises()

    const catchupTrigger = editWrapper.find('#schedule-catchup')
    expect(catchupTrigger.text()).toContain('Once')

    await editWrapper.find('form').trigger('submit')
    await flushPromises()
    const patch = editCalls.find(c => c.url === `/api/schedules/${existingPipelineSchedule.id}` && c.init?.method === 'PATCH')
    expect(JSON.parse(String(patch?.init?.body)).catchup).toBe('once')
    editWrapper.unmount()
  })

  it('keeps radio group names unique across two mounted forms', async () => {
    const calls: Array<{ url: string, init?: RequestInit }> = []
    stubFetch({ id: 's1' }, calls)

    const wrapperA = mount(ScheduleForm)
    const wrapperB = mount(ScheduleForm)
    await flushPromises()

    const nameA = wrapperA.find('[data-testid="schedule-run-mode-job"]').attributes('name')
    const nameB = wrapperB.find('[data-testid="schedule-run-mode-job"]').attributes('name')
    expect(nameA).toBeDefined()
    expect(nameA).not.toBe(nameB)

    wrapperA.unmount()
    wrapperB.unmount()
  })
})
