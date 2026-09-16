import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ApplicationSettings from '@/features/settings/components/ApplicationSettings.vue'

const MAIL = {
  resourceId: 'res-mail',
  serverName: 'mail',
  attachAll: false,
  requiredEnv: ['MAIL_PASSWORD'],
  secrets: [],
  tools: [
    { capability: 'mcp__mail__search', name: 'search', readOnlyHint: true },
    { capability: 'mcp__mail__send', name: 'send', readOnlyHint: false, destructiveHint: true },
  ],
}

function stubFetch(responses: Record<string, unknown>) {
  const calls: Array<{ url: string, init?: RequestInit }> = []
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    calls.push({ url, init })
    const body = responses[`${init?.method ?? 'GET'} ${url}`]
    return { ok: true, status: body === undefined ? 204 : 200, json: async () => body }
  }))
  return calls
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('applicationSettings', () => {
  it('lists applications with their tools and marks a missing required secret', async () => {
    stubFetch({ 'GET /api/applications': [MAIL] })
    const wrapper = mount(ApplicationSettings)
    await flushPromises()

    expect(wrapper.text()).toContain('mail')
    expect(wrapper.text()).toContain('mcp__mail__search')
    expect(wrapper.find('[data-testid="secret-missing-MAIL_PASSWORD"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('shows the empty state when no server is registered', async () => {
    stubFetch({ 'GET /api/applications': [] })
    const wrapper = mount(ApplicationSettings)
    await flushPromises()
    expect(wrapper.find('[data-testid="applications-empty"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('sends a secret and never renders its value afterwards', async () => {
    const calls = stubFetch({ 'GET /api/applications': [MAIL] })
    const wrapper = mount(ApplicationSettings)
    await flushPromises()

    await wrapper.find('[data-testid="secret-input-res-mail-MAIL_PASSWORD"]').setValue('hunter2')
    await wrapper.find('[data-testid="secret-save-res-mail-MAIL_PASSWORD"]').trigger('click')
    await flushPromises()

    const put = calls.find(c => c.init?.method === 'PUT')
    expect(put?.url).toBe('/api/applications/res-mail/secrets/MAIL_PASSWORD')
    expect(JSON.parse(String(put?.init?.body))).toEqual({ value: 'hunter2' })
    expect(wrapper.html()).not.toContain('hunter2')
    wrapper.unmount()
  })
})
