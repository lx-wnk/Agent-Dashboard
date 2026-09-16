import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ScheduleForm from '@/components/ScheduleForm.vue'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('scheduleForm — applications', () => {
  it('sends the applications the user ticked', async () => {
    const calls: Array<{ url: string, init?: RequestInit }> = []
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      calls.push({ url, init })
      if (url === '/api/applications') {
        return { ok: true, status: 200, json: async () => [
          { resourceId: 'res-mail', serverName: 'mail', attachAll: false, requiredEnv: [], secrets: [], tools: [] },
          { resourceId: 'res-notes', serverName: 'notes', attachAll: true, requiredEnv: [], secrets: [], tools: [] },
        ] }
      }
      return { ok: true, status: 200, json: async () => ({ id: 's1', applications: ['res-mail'] }) }
    }))

    const wrapper = mount(ScheduleForm)
    await flushPromises()

    expect(wrapper.find('[data-testid="schedule-application-res-notes"]').attributes('disabled')).toBeDefined()
    await wrapper.find('[data-testid="schedule-application-res-mail"]').setValue(true)
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    const post = calls.find(c => c.url === '/api/schedules' && c.init?.method === 'POST')
    expect(JSON.parse(String(post?.init?.body)).applications).toEqual(['res-mail'])
    wrapper.unmount()
  })
})
