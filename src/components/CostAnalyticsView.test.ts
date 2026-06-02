import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import CostAnalyticsView from './CostAnalyticsView.vue'

// Minimal EventSource stub — jsdom has no native EventSource.
class MockEventSource {
  static instances: MockEventSource[] = []
  onmessage: ((e: MessageEvent) => void) | null = null
  onerror: ((e: Event) => void) | null = null
  readyState = 0

  constructor(public url: string) {
    MockEventSource.instances.push(this)
  }

  close() {
    this.readyState = 2
  }
}

const emptyApiResponse = {
  byModel: [],
  byDay: [],
  byWeek: [],
  totalUsd: 0,
  updatedAt: 0,
}

beforeEach(() => {
  MockEventSource.instances = []
  vi.stubGlobal('EventSource', MockEventSource)
})

describe('costAnalyticsView', () => {
  it('renders empty-state message when API returns no rows', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => emptyApiResponse,
    }))
    const wrapper = mount(CostAnalyticsView)
    await flushPromises()
    expect(wrapper.text()).toContain('Cost Analytics')
    expect(wrapper.text()).toContain('No cost data yet')
  })

  it('empty state contains accurate copy about automatic import', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => emptyApiResponse,
    }))
    const wrapper = mount(CostAnalyticsView)
    await flushPromises()
    const text = wrapper.text()
    // Must NOT contain the old false claim about stage agents
    expect(text).not.toContain('Once stage agents finish runs')
    // Must contain the accurate description
    expect(text).toContain('imported automatically from your Claude sessions')
  })

  it('empty state renders a "Rescan now" button', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => emptyApiResponse,
    }))
    const wrapper = mount(CostAnalyticsView)
    await flushPromises()
    const buttons = wrapper.findAll('button')
    const rescanBtn = buttons.find(b => b.text().includes('Rescan now'))
    expect(rescanBtn).toBeTruthy()
  })

  it('clicking "Rescan now" issues POST /api/history/import', async () => {
    const mockFetch = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: async () => emptyApiResponse,
      })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({}),
      })
    vi.stubGlobal('fetch', mockFetch)

    const wrapper = mount(CostAnalyticsView)
    await flushPromises()

    const buttons = wrapper.findAll('button')
    const rescanBtn = buttons.find(b => b.text().includes('Rescan now'))
    expect(rescanBtn).toBeTruthy()
    await rescanBtn!.trigger('click')
    await flushPromises()

    expect(mockFetch).toHaveBeenCalledWith('/api/history/import', { method: 'POST' })
  })

  it('button is disabled while a scan is in flight', async () => {
    // First fetch: summary (returns no data), second fetch: POST import (stays pending to simulate in-flight)
    let resolveImport!: () => void
    const importPending = new Promise<void>(res => (resolveImport = res))

    const mockFetch = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: async () => emptyApiResponse,
      })
      .mockReturnValueOnce(
        importPending.then(() => ({ ok: true, json: async () => ({}) })),
      )
    vi.stubGlobal('fetch', mockFetch)

    const wrapper = mount(CostAnalyticsView)
    await flushPromises()

    const buttons = wrapper.findAll('button')
    const rescanBtn = buttons.find(b => b.text().includes('Rescan now'))!
    await rescanBtn.trigger('click')
    // Don't await flushPromises — import POST is still in flight

    // The button should be disabled (isImporting = true) and show "Scanning…"
    expect(rescanBtn.attributes('disabled')).toBeDefined()
    expect(rescanBtn.text()).toContain('Scanning')

    // Clean up
    resolveImport()
  })

  it('calls refresh() when stream reports done:true', async () => {
    // fetch call #1: summary (empty), fetch call #2: POST import (ok)
    // fetch call #3: refresh() triggers another summary fetch
    const mockFetch = vi.fn()
      .mockResolvedValue({
        ok: true,
        json: async () => emptyApiResponse,
      })
    vi.stubGlobal('fetch', mockFetch)

    const wrapper = mount(CostAnalyticsView)
    await flushPromises()

    const buttons = wrapper.findAll('button')
    const rescanBtn = buttons.find(b => b.text().includes('Rescan now'))!
    await rescanBtn.trigger('click')
    await flushPromises()

    // Simulate SSE stream: one progress event then done
    const es = MockEventSource.instances[MockEventSource.instances.length - 1]
    expect(es).toBeTruthy()
    expect(es.url).toBe('/api/history/import/status')

    es.onmessage?.(new MessageEvent('message', {
      data: JSON.stringify({ total: 10, processed: 5, imported: 5, errors: 0, done: false }),
    }))
    await flushPromises()

    es.onmessage?.(new MessageEvent('message', {
      data: JSON.stringify({ total: 10, processed: 10, imported: 10, errors: 0, done: true }),
    }))
    await flushPromises()

    // EventSource should be closed
    expect(es.readyState).toBe(2)
    // refresh() should have fired — fetch called again (beyond the initial + POST)
    expect(mockFetch.mock.calls.length).toBeGreaterThanOrEqual(3)
    // importStatus shows imported count
    expect(wrapper.text()).toContain('Imported 10 sessions')
  })

  it('renders model breakdown and total cost when API returns data', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        byModel: [
          { model: 'claude-opus-4', costUsd: 1.5, sessions: 2 },
          { model: 'claude-sonnet-4', costUsd: 0.3, sessions: 1 },
        ],
        byDay: [
          { day: '2026-05-22', model: 'claude-opus-4', costUsd: 1.5 },
          { day: '2026-05-23', model: 'claude-sonnet-4', costUsd: 0.3 },
        ],
        byWeek: [
          { week: '2026-W21', costUsd: 1.8 },
        ],
        totalUsd: 1.8,
        updatedAt: Date.now(),
      }),
    }))
    const wrapper = mount(CostAnalyticsView)
    await flushPromises()
    const text = wrapper.text()
    expect(text).toContain('Spend by Model')
    expect(text).toContain('claude-opus-4')
    expect(text).toContain('claude-sonnet-4')
    // formatCost output should contain the dollar amount
    expect(text).toMatch(/\$1\.80|\$1\.8/)
  })

  it('renders error state when fetch fails', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      text: async () => 'boom',
    }))
    const wrapper = mount(CostAnalyticsView)
    await flushPromises()
    expect(wrapper.text()).toContain('HTTP 500')
  })
})
