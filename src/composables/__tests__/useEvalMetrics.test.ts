import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'

// Minimal localStorage stub
const store: Record<string, string> = {}
globalThis.localStorage = {
  getItem: (k: string) => store[k] ?? null,
  setItem: (k: string, v: string) => { store[k] = v },
  removeItem: (k: string) => { delete store[k] },
  clear: () => { Object.keys(store).forEach(k => delete store[k]) },
  length: 0,
  key: () => null,
}

function makeSnapshot(metricKey: string) {
  return {
    id: '1',
    spawnerId: 'sp1',
    model: 'claude-opus-4',
    stage: 'implementation',
    metricKey,
    value: 0.85,
    sampleCount: 10,
    windowStart: '2026-06-14T00:00:00Z',
    windowEnd: '2026-06-15T00:00:00Z',
    recordedAt: '2026-06-15T00:00:00Z',
  }
}

const alertOpen = {
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

function withSetup<T>(composable: () => T) {
  let result!: T
  const Wrapper = defineComponent({
    setup() {
      result = composable()
      return {}
    },
    template: '<div />',
  })
  mount(Wrapper)
  return result
}

let useEvalMetrics: typeof import('../useEvalMetrics')

beforeEach(async () => {
  vi.resetModules()
  useEvalMetrics = await import('../useEvalMetrics')
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('useEvalMetrics', () => {
  it('fetches metrics and alerts on start', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => [],
    })
    vi.stubGlobal('fetch', mockFetch)

    const { start, isLoading } = withSetup(() => useEvalMetrics.useEvalMetrics())
    expect(isLoading.value).toBe(true)
    start()
    await flushPromises()
    expect(isLoading.value).toBe(false)
    // 9 metric fetches + 1 alerts fetch
    expect(mockFetch.mock.calls.length).toBeGreaterThanOrEqual(10)
  })

  it('populates snapshots grouped by metricKey', async () => {
    const mockFetch = vi.fn().mockImplementation((url: string) => {
      if (String(url).includes('/api/eval/metrics')) {
        const key = new URL(url, 'http://x').searchParams.get('metric') ?? ''
        return Promise.resolve({ ok: true, json: async () => [makeSnapshot(key)] })
      }
      return Promise.resolve({ ok: true, json: async () => [] })
    })
    vi.stubGlobal('fetch', mockFetch)

    const { snapshots, start } = withSetup(() => useEvalMetrics.useEvalMetrics())
    start()
    await flushPromises()
    expect(snapshots.value.success_rate).toHaveLength(1)
    expect(snapshots.value.success_rate[0].value).toBe(0.85)
  })

  it('populates openAlerts', async () => {
    const mockFetch = vi.fn().mockImplementation((url: string) => {
      if (String(url).includes('/api/eval/drift'))
        return Promise.resolve({ ok: true, json: async () => [alertOpen] })
      return Promise.resolve({ ok: true, json: async () => [] })
    })
    vi.stubGlobal('fetch', mockFetch)

    const { openAlerts, start } = withSetup(() => useEvalMetrics.useEvalMetrics())
    start()
    await flushPromises()
    expect(openAlerts.value).toHaveLength(1)
    expect(openAlerts.value[0].id).toBe('a1')
  })

  it('sets error when fetch fails', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 500 }))

    const { error, start } = withSetup(() => useEvalMetrics.useEvalMetrics())
    start()
    await flushPromises()
    expect(error.value).toContain('HTTP 500')
  })

  it('acknowledge POSTs to /api/eval/drift/{id}/ack then refreshes alerts', async () => {
    const mockFetch = vi.fn()
      .mockImplementation((url: string, opts?: RequestInit) => {
        if (opts?.method === 'POST' && String(url).includes('/ack'))
          return Promise.resolve({ ok: true, json: async () => ({}) })
        if (String(url).includes('/api/eval/drift'))
          return Promise.resolve({ ok: true, json: async () => [] })
        return Promise.resolve({ ok: true, json: async () => [] })
      })
    vi.stubGlobal('fetch', mockFetch)

    const { acknowledge, start } = withSetup(() => useEvalMetrics.useEvalMetrics())
    start()
    await flushPromises()

    await acknowledge('a1')
    await flushPromises()

    const ackCall = (mockFetch.mock.calls as [string, RequestInit][]).find(([url, opts]) =>
      String(url).includes('/api/eval/drift/a1/ack') && opts?.method === 'POST',
    )
    expect(ackCall).toBeTruthy()
  })

  it('runScan POSTs to /api/eval/scan then re-fetches', async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, json: async () => [] })
    vi.stubGlobal('fetch', mockFetch)

    const { runScan, start } = withSetup(() => useEvalMetrics.useEvalMetrics())
    start()
    await flushPromises()

    const callsBefore = mockFetch.mock.calls.length
    await runScan()
    await flushPromises()

    const scanCall = (mockFetch.mock.calls as [string, RequestInit][]).find(([url, opts]) =>
      String(url).includes('/api/eval/scan') && opts?.method === 'POST',
    )
    expect(scanCall).toBeTruthy()
    // re-fetch should have added more calls
    expect(mockFetch.mock.calls.length).toBeGreaterThan(callsBefore)
  })
})
