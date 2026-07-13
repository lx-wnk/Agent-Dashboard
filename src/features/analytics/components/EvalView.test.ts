import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { toast } from '@/composables/useToast'
import EvalView from '@/features/analytics/components/EvalView.vue'

const emptyAlerts: unknown[] = []

const openAlert = {
  id: 'a1',
  spawnerId: 'sp1',
  model: 'claude-opus-4',
  stage: 'implementation',
  metricKey: 'success_rate',
  status: 'open',
  direction: 'down',
  baselineValue: 0.9,
  recentValue: 0.7,
  delta: -0.2,
  threshold: 0.1,
  sampleCount: 10,
  detectedAt: '2026-06-15T00:00:00Z',
  acknowledgedAt: null,
}

function mockFetchFactory(alerts: unknown[]) {
  return vi.fn().mockImplementation((url: string) => {
    if (String(url).includes('/api/eval/drift'))
      return Promise.resolve({ ok: true, json: async () => alerts })
    return Promise.resolve({ ok: true, json: async () => [] })
  })
}

beforeEach(() => {
  vi.stubGlobal('fetch', mockFetchFactory(emptyAlerts))
})

describe('evalView', () => {
  it('renders the heading', async () => {
    const wrapper = mount(EvalView)
    await flushPromises()
    expect(wrapper.text()).toContain('Eval / Drift Detection')
  })

  it('renders a "Run scan now" button', async () => {
    const wrapper = mount(EvalView)
    await flushPromises()
    const btn = wrapper.findAll('button').find(b => b.text().includes('Run scan now'))
    expect(btn).toBeTruthy()
  })

  it('shows empty-state message when no alerts', async () => {
    const wrapper = mount(EvalView)
    await flushPromises()
    expect(wrapper.text()).toContain('No open drift alerts')
  })

  it('renders open alerts with metric label and direction', async () => {
    vi.stubGlobal('fetch', mockFetchFactory([openAlert]))
    const wrapper = mount(EvalView)
    await flushPromises()
    const text = wrapper.text()
    expect(text).toContain('Success rate')
    expect(text).toContain('0.900')
    expect(text).toContain('0.700')
    expect(text).toContain('n=10')
  })

  it('renders acknowledge button for each open alert', async () => {
    vi.stubGlobal('fetch', mockFetchFactory([openAlert]))
    const wrapper = mount(EvalView)
    await flushPromises()
    const ackBtn = wrapper.findAll('button').find(b => b.text() === 'Acknowledge')
    expect(ackBtn).toBeTruthy()
  })

  it('clicking "Acknowledge" calls POST /api/eval/drift/{id}/ack', async () => {
    const mockFetch = vi.fn()
      .mockImplementation((url: string, opts?: RequestInit) => {
        if (opts?.method === 'POST' && String(url).includes('/ack'))
          return Promise.resolve({ ok: true, json: async () => ({}) })
        if (String(url).includes('/api/eval/drift'))
          return Promise.resolve({ ok: true, json: async () => [openAlert] })
        return Promise.resolve({ ok: true, json: async () => [] })
      })
    vi.stubGlobal('fetch', mockFetch)

    const wrapper = mount(EvalView)
    await flushPromises()

    const ackBtn = wrapper.findAll('button').find(b => b.text() === 'Acknowledge')
    expect(ackBtn).toBeTruthy()
    await ackBtn!.trigger('click')
    await flushPromises()

    const ackCall = (mockFetch.mock.calls as [string, RequestInit][]).find(([url, opts]) =>
      String(url).includes('/api/eval/drift/a1/ack') && opts?.method === 'POST',
    )
    expect(ackCall).toBeTruthy()
  })

  it('clicking "Run scan now" calls POST /api/eval/scan', async () => {
    const mockFetch = vi.fn()
      .mockImplementation((url: string, opts?: RequestInit) => {
        if (opts?.method === 'POST' && String(url).includes('/api/eval/scan'))
          return Promise.resolve({ ok: true, json: async () => ({ ok: true }) })
        if (String(url).includes('/api/eval/drift'))
          return Promise.resolve({ ok: true, json: async () => [] })
        return Promise.resolve({ ok: true, json: async () => [] })
      })
    vi.stubGlobal('fetch', mockFetch)

    const wrapper = mount(EvalView)
    await flushPromises()

    const scanBtn = wrapper.findAll('button').find(b => b.text().includes('Run scan now'))
    expect(scanBtn).toBeTruthy()
    await scanBtn!.trigger('click')
    await flushPromises()

    const scanCall = (mockFetch.mock.calls as [string, RequestInit][]).find(([url, opts]) =>
      String(url).includes('/api/eval/scan') && opts?.method === 'POST',
    )
    expect(scanCall).toBeTruthy()
  })

  it('surfaces a toast when fetch fails', async () => {
    const errorSpy = vi.spyOn(toast, 'error')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 503 }))
    const wrapper = mount(EvalView)
    await flushPromises()
    expect(errorSpy).toHaveBeenCalledWith(expect.stringContaining('503'))
    expect(wrapper.find('.text-danger-text').exists()).toBe(false)
  })

  it('renders dimension label grouping for alerts', async () => {
    vi.stubGlobal('fetch', mockFetchFactory([openAlert]))
    const wrapper = mount(EvalView)
    await flushPromises()
    expect(wrapper.text()).toContain('sp1 / claude-opus-4 / implementation')
  })
})
