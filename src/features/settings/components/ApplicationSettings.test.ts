import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ApplicationSettings from '@/features/settings/components/ApplicationSettings.vue'

class MockEventSource {
  static instances: MockEventSource[] = []
  onmessage: ((e: MessageEvent) => void) | null = null
  onerror: ((e: Event) => void) | null = null
  readyState = 0
  static CONNECTING = 0
  static OPEN = 1
  static CLOSED = 2
  constructor(public url: string) { MockEventSource.instances.push(this) }
  close() { this.readyState = 2 }
}

beforeEach(() => {
  MockEventSource.instances = []
  vi.stubGlobal('EventSource', MockEventSource)
})

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
  entry: { command: 'node', args: ['mail-server.js'], env: {} },
  exportToClaude: false,
}

function stubFetch(responses: Record<string, unknown>) {
  const calls: Array<{ url: string, init?: RequestInit }> = []
  const merged: Record<string, unknown> = { 'GET /api/applications/drift': { found: [], changed: [] }, ...responses }
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    calls.push({ url, init })
    const body = merged[`${init?.method ?? 'GET'} ${url}`]
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

  it('adds a required variable', async () => {
    const calls = stubFetch({
      'GET /api/applications': [{ ...MAIL, requiredEnv: [] }],
      'PATCH /api/applications/res-mail': { ...MAIL, requiredEnv: ['MAIL_PASSWORD'] },
    })
    const wrapper = mount(ApplicationSettings)
    await flushPromises()

    expect(wrapper.text()).toContain('No secrets declared. Add the environment variable names this server reads its credentials from.')

    await wrapper.find('[data-testid="required-env-input-res-mail"]').setValue('MAIL_PASSWORD')
    await wrapper.find('[data-testid="required-env-add-res-mail"]').trigger('click')
    await flushPromises()

    const patch = calls.find(c => c.init?.method === 'PATCH')
    expect(JSON.parse(String(patch?.init?.body))).toEqual({ requiredEnv: ['MAIL_PASSWORD'] })
    expect(wrapper.find('[data-testid="secret-input-res-mail-MAIL_PASSWORD"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('rejects an invalid name without a request', async () => {
    const calls = stubFetch({ 'GET /api/applications': [{ ...MAIL, requiredEnv: [] }] })
    const wrapper = mount(ApplicationSettings)
    await flushPromises()

    await wrapper.find('[data-testid="required-env-input-res-mail"]').setValue('mail-password')
    await wrapper.find('[data-testid="required-env-add-res-mail"]').trigger('click')
    await flushPromises()

    expect(calls.some(c => c.init?.method === 'PATCH')).toBe(false)
    expect(wrapper.text()).toContain('is not an environment variable name')
    wrapper.unmount()
  })

  it('stops requiring a variable', async () => {
    const calls = stubFetch({
      'GET /api/applications': [MAIL],
      'PATCH /api/applications/res-mail': { ...MAIL, requiredEnv: [] },
    })
    const wrapper = mount(ApplicationSettings)
    await flushPromises()

    await wrapper.find('[data-testid="required-env-remove-res-mail-MAIL_PASSWORD"]').trigger('click')
    await flushPromises()

    const patch = calls.find(c => c.init?.method === 'PATCH')
    expect(JSON.parse(String(patch?.init?.body))).toEqual({ requiredEnv: [] })
    wrapper.unmount()
  })

  it('adds a server via the form and refetches the list', async () => {
    const calls = stubFetch({
      'GET /api/applications': [MAIL],
      'POST /api/applications': { ...MAIL, resourceId: 'res-cal', serverName: 'calendar' },
    })
    const wrapper = mount(ApplicationSettings)
    await flushPromises()

    await wrapper.find('[data-testid="application-add-toggle"]').trigger('click')
    await wrapper.find('[data-testid="application-add-name"]').setValue('calendar')
    await wrapper.find('[data-testid="application-add-command"]').setValue('npx')
    await wrapper.find('[data-testid="application-add-args"]').setValue('-y calendar-mcp')
    await wrapper.find('[data-testid="application-add-env"]').setValue('API_KEY=abc')
    await wrapper.find('[data-testid="application-add-form"]').trigger('submit')
    await flushPromises()

    const post = calls.find(c => c.init?.method === 'POST')
    expect(post?.url).toBe('/api/applications')
    expect(JSON.parse(String(post?.init?.body))).toEqual({
      name: 'calendar',
      command: 'npx',
      args: ['-y', 'calendar-mcp'],
      env: { API_KEY: 'abc' },
    })
    expect(calls.filter(c => (c.init?.method ?? 'GET') === 'GET' && c.url === '/api/applications').length).toBe(2)
    wrapper.unmount()
  })

  it('shows a create error and leaves the form open', async () => {
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === 'POST')
        return { ok: false, status: 400, json: async () => ({ error: 'slug must match ^[a-z0-9-]+$' }) }
      return { ok: true, status: 200, json: async () => [MAIL] }
    }))
    const wrapper = mount(ApplicationSettings)
    await flushPromises()

    await wrapper.find('[data-testid="application-add-toggle"]').trigger('click')
    await wrapper.find('[data-testid="application-add-name"]').setValue('Bad Name!')
    await wrapper.find('[data-testid="application-add-command"]').setValue('npx')
    await wrapper.find('[data-testid="application-add-form"]').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('slug must match')
    expect(wrapper.find('[data-testid="application-add-form"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('edits the server entry', async () => {
    const calls = stubFetch({
      'GET /api/applications': [MAIL],
      'PATCH /api/applications/res-mail': { ...MAIL, entry: { command: 'node', args: ['server.js'], env: { FOO: 'bar' } } },
    })
    const wrapper = mount(ApplicationSettings)
    await flushPromises()

    await wrapper.find('[data-testid="application-edit-res-mail"]').trigger('click')
    await wrapper.find('[data-testid="application-edit-command-res-mail"]').setValue('node')
    await wrapper.find('[data-testid="application-edit-args-res-mail"]').setValue('server.js')
    await wrapper.find('[data-testid="application-edit-env-res-mail"]').setValue('FOO=bar')
    await wrapper.find('[data-testid="application-edit-save-res-mail"]').trigger('click')
    await flushPromises()

    const patch = calls.find(c => c.init?.method === 'PATCH')
    expect(JSON.parse(String(patch?.init?.body))).toEqual({
      entry: { command: 'node', args: ['server.js'], env: { FOO: 'bar' } },
    })
    wrapper.unmount()
  })

  it('toggles export to Claude and reflects the response', async () => {
    const calls = stubFetch({
      'GET /api/applications': [MAIL],
      'PATCH /api/applications/res-mail': { ...MAIL, exportToClaude: true },
    })
    const wrapper = mount(ApplicationSettings)
    await flushPromises()

    const toggle = wrapper.find('[data-testid="application-export-res-mail"]')
    expect(toggle.attributes('aria-checked')).toBe('false')
    await toggle.trigger('click')
    await flushPromises()

    const patch = calls.find(c => c.init?.method === 'PATCH')
    expect(JSON.parse(String(patch?.init?.body))).toEqual({ exportToClaude: true })
    expect(wrapper.find('[data-testid="application-export-res-mail"]').attributes('aria-checked')).toBe('true')
    wrapper.unmount()
  })

  it('shows a 502 export error and keeps the switch off', async () => {
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === 'PATCH')
        return { ok: false, status: 502, json: async () => ({ error: 'failed to write Claude config' }) }
      return { ok: true, status: 200, json: async () => [MAIL] }
    }))
    const wrapper = mount(ApplicationSettings)
    await flushPromises()

    await wrapper.find('[data-testid="application-export-res-mail"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('failed to write Claude config')
    expect(wrapper.find('[data-testid="application-export-res-mail"]').attributes('aria-checked')).toBe('false')
    wrapper.unmount()
  })

  it('removes a server after confirming in place', async () => {
    const calls = stubFetch({ 'GET /api/applications': [MAIL] })
    const wrapper = mount(ApplicationSettings)
    await flushPromises()

    expect(wrapper.find('[data-testid="application-remove-confirm-res-mail"]').exists()).toBe(false)
    await wrapper.find('[data-testid="application-remove-res-mail"]').trigger('click')
    expect(wrapper.find('[data-testid="application-remove-confirm-res-mail"]').exists()).toBe(true)

    await wrapper.find('[data-testid="application-remove-confirm-res-mail"]').trigger('click')
    await flushPromises()

    const del = calls.find(c => c.init?.method === 'DELETE')
    expect(del?.url).toBe('/api/applications/res-mail')
    wrapper.unmount()
  })

  it('shows a 409 error naming attached routines and keeps the card', async () => {
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === 'DELETE')
        return { ok: false, status: 409, json: async () => ({ error: 'still attached to: inbox' }) }
      return { ok: true, status: 200, json: async () => [MAIL] }
    }))
    const wrapper = mount(ApplicationSettings)
    await flushPromises()

    await wrapper.find('[data-testid="application-remove-res-mail"]').trigger('click')
    await wrapper.find('[data-testid="application-remove-confirm-res-mail"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('still attached to: inbox')
    expect(wrapper.find('[data-testid="application-res-mail"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('imports a found server from the drift banner', async () => {
    const calls = stubFetch({
      'GET /api/applications': [],
      'GET /api/applications/drift': { found: ['notes'], changed: [] },
      'POST /api/applications/import': { ...MAIL, resourceId: 'res-notes', serverName: 'notes' },
    })
    const wrapper = mount(ApplicationSettings)
    await flushPromises()

    const banner = wrapper.find('[data-testid="application-drift-found-notes"]')
    expect(banner.text()).toContain('Found notes')

    const getCountBefore = calls.filter(c => (c.init?.method ?? 'GET') === 'GET').length
    await banner.find('button').trigger('click')
    await flushPromises()

    const post = calls.find(c => c.init?.method === 'POST' && c.url === '/api/applications/import')
    expect(JSON.parse(String(post?.init?.body))).toEqual({ name: 'notes' })
    const getCountAfter = calls.filter(c => (c.init?.method ?? 'GET') === 'GET').length
    expect(getCountAfter).toBe(getCountBefore + 2)
    expect(calls.some(c => (c.init?.method ?? 'GET') === 'GET' && c.url === '/api/applications')).toBe(true)
    expect(calls.some(c => (c.init?.method ?? 'GET') === 'GET' && c.url === '/api/applications/drift')).toBe(true)
    wrapper.unmount()
  })

  it('takes the file version by disabling export', async () => {
    const calls = stubFetch({
      'GET /api/applications': [MAIL],
      'GET /api/applications/drift': { found: [], changed: ['mail'] },
      'PATCH /api/applications/res-mail': { ...MAIL, exportToClaude: false },
    })
    const wrapper = mount(ApplicationSettings)
    await flushPromises()

    const banner = wrapper.find('[data-testid="application-drift-changed-mail"]')
    expect(banner.text()).toContain('mail')
    const buttons = banner.findAll('button')
    expect(buttons).toHaveLength(2)
    await buttons[0].trigger('click')
    await flushPromises()

    const patch = calls.find(c => c.init?.method === 'PATCH')
    expect(patch?.url).toBe('/api/applications/res-mail')
    expect(JSON.parse(String(patch?.init?.body))).toEqual({ exportToClaude: false })
    wrapper.unmount()
  })

  it('writes the app version back to Claude config', async () => {
    const calls = stubFetch({
      'GET /api/applications': [MAIL],
      'GET /api/applications/drift': { found: [], changed: ['mail'] },
      'PATCH /api/applications/res-mail': { ...MAIL, exportToClaude: true },
    })
    const wrapper = mount(ApplicationSettings)
    await flushPromises()

    const banner = wrapper.find('[data-testid="application-drift-changed-mail"]')
    const buttons = banner.findAll('button')
    await buttons[1].trigger('click')
    await flushPromises()

    const patch = calls.find(c => c.init?.method === 'PATCH')
    expect(patch?.url).toBe('/api/applications/res-mail')
    expect(JSON.parse(String(patch?.init?.body))).toEqual({ exportToClaude: true })
    wrapper.unmount()
  })

  it('shows no drift banner when nothing changed', async () => {
    stubFetch({ 'GET /api/applications': [MAIL] })
    const wrapper = mount(ApplicationSettings)
    await flushPromises()

    expect(wrapper.find('[data-testid^="application-drift-"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('refetches drift on an applications_changed event', async () => {
    const calls = stubFetch({ 'GET /api/applications': [MAIL] })
    const wrapper = mount(ApplicationSettings)
    await flushPromises()

    const driftCallsBefore = calls.filter(c => (c.init?.method ?? 'GET') === 'GET' && c.url === '/api/applications/drift').length
    expect(driftCallsBefore).toBe(1)

    MockEventSource.instances[0].onmessage?.({ data: JSON.stringify({ type: 'applications_changed' }) } as MessageEvent)
    await flushPromises()

    const driftCallsAfter = calls.filter(c => (c.init?.method ?? 'GET') === 'GET' && c.url === '/api/applications/drift').length
    expect(driftCallsAfter).toBe(2)
    wrapper.unmount()
  })

  it('keeps the list and shows no panel error when the drift fetch fails', async () => {
    vi.stubGlobal('fetch', vi.fn(async (url: string) => {
      if (url === '/api/applications/drift')
        throw new Error('network down')
      return { ok: true, status: 200, json: async () => [MAIL] }
    }))
    const wrapper = mount(ApplicationSettings)
    await flushPromises()

    expect(wrapper.text()).toContain('mail')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('closes the event stream on unmount', async () => {
    stubFetch({ 'GET /api/applications': [MAIL] })
    const wrapper = mount(ApplicationSettings)
    await flushPromises()

    expect(MockEventSource.instances).toHaveLength(1)
    wrapper.unmount()

    expect(MockEventSource.instances[0].readyState).toBe(MockEventSource.CLOSED)
  })
})
