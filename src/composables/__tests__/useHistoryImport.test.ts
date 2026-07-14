import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { useHistoryImport } from '../useHistoryImport'

class MockEventSource {
  static instances: MockEventSource[] = []
  onmessage: ((e: MessageEvent) => void) | null = null
  onerror: ((e: Event) => void) | null = null
  readyState = 0
  static CONNECTING = 0
  static OPEN = 1
  static CLOSED = 2

  constructor(public url: string) {
    MockEventSource.instances.push(this)
  }

  close() {
    this.readyState = 2
  }
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
  const wrapper = mount(Wrapper, { attachTo: document.body })
  return { result, wrapper }
}

beforeEach(() => {
  MockEventSource.instances = []
  vi.stubGlobal('EventSource', MockEventSource)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useHistoryImport', () => {
  it('starts the scan and attaches the SSE stream on success', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true }))
    const { result, wrapper } = withSetup(() => useHistoryImport())

    const p = result.start()
    expect(result.isImporting.value).toBe(true)
    expect(result.importStatus.value).toBe('Starting…')
    await p

    expect(fetch).toHaveBeenCalledWith('/api/history/import', { method: 'POST' })
    expect(result.importStatus.value).toBe('Scanning…')
    expect(MockEventSource.instances).toHaveLength(1)
    expect(MockEventSource.instances[0].url).toBe('/api/history/import/status')
    wrapper.unmount()
  })

  it('does nothing if a scan is already in flight', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true }))
    const { result, wrapper } = withSetup(() => useHistoryImport())

    const p1 = result.start()
    void result.start()
    await p1

    expect(fetch).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('attaches the stream and stays importing on a 409 (already running)', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 409,
      json: () => Promise.resolve({ error: 'Import already running' }),
    }))
    const { result, wrapper } = withSetup(() => useHistoryImport())

    await result.start()

    expect(result.isImporting.value).toBe(true)
    expect(result.importStatus.value).toBe('Import already running — watching progress…')
    expect(MockEventSource.instances).toHaveLength(1)
    wrapper.unmount()
  })

  it('surfaces a hard error and does not attach a stream', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      json: () => Promise.resolve({ error: 'boom' }),
    }))
    const { result, wrapper } = withSetup(() => useHistoryImport())

    await result.start()

    expect(result.isImporting.value).toBe(false)
    expect(result.importStatus.value).toBe('Error: boom')
    expect(MockEventSource.instances).toHaveLength(0)
    wrapper.unmount()
  })

  it('surfaces a network error thrown by fetch', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('network down')))
    const { result, wrapper } = withSetup(() => useHistoryImport())

    await result.start()

    expect(result.isImporting.value).toBe(false)
    expect(result.importStatus.value).toBe('Error: network down')
    wrapper.unmount()
  })

  it('updates progress on incoming SSE messages and stops on done', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true }))
    const onDone = vi.fn()
    const { result, wrapper } = withSetup(() => useHistoryImport({ onDone }))
    await result.start()

    const es = MockEventSource.instances[0]
    es.onmessage?.(new MessageEvent('message', {
      data: JSON.stringify({ total: 10, processed: 4, done: false }),
    }))
    expect(result.importStatus.value).toBe('Scanning… 4/10')
    expect(result.isImporting.value).toBe(true)

    es.onmessage?.(new MessageEvent('message', {
      data: JSON.stringify({ total: 10, processed: 10, imported: 9, errors: 1, done: true }),
    }))
    expect(result.importStatus.value).toBe('Imported 9 sessions')
    expect(result.isImporting.value).toBe(false)
    expect(onDone).toHaveBeenCalledWith({ total: 10, processed: 10, imported: 9, errors: 1, done: true })
    wrapper.unmount()
  })

  it('treats a malformed SSE frame as a stream error', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true }))
    const { result, wrapper } = withSetup(() => useHistoryImport())
    await result.start()

    const es = MockEventSource.instances[0]
    es.onmessage?.(new MessageEvent('message', { data: 'not json' }))

    expect(result.importStatus.value).toBe('Connection lost — import may still be running')
    expect(result.isImporting.value).toBe(false)
    wrapper.unmount()
  })

  it('treats an SSE connection error the same as a malformed frame', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true }))
    const { result, wrapper } = withSetup(() => useHistoryImport())
    await result.start()

    const es = MockEventSource.instances[0]
    es.onerror?.(new Event('error'))

    expect(result.importStatus.value).toBe('Connection lost — import may still be running')
    expect(result.isImporting.value).toBe(false)
    wrapper.unmount()
  })

  it('closes the stream on unmount', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true }))
    const { result, wrapper } = withSetup(() => useHistoryImport())
    await result.start()

    const es = MockEventSource.instances[0]
    wrapper.unmount()

    expect(es.readyState).toBe(MockEventSource.CLOSED)
    expect(result.isImporting.value).toBe(false)
  })
})
