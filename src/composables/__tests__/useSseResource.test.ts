import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createSseResource } from '../useSseResource'
import { SSE_FALLBACK_POLL_MS, SSE_RETRY_DELAY_MS } from '../../utils/sse'

class MockEventSource {
  static instances: MockEventSource[] = []
  static CONNECTING = 0
  static OPEN = 1
  static CLOSED = 2
  onmessage: ((e: MessageEvent) => void) | null = null
  onerror: ((e: Event) => void) | null = null
  readyState = 0

  constructor(public url: string) {
    MockEventSource.instances.push(this)
  }

  emit(data: string) {
    this.onmessage?.({ data } as MessageEvent)
  }

  fail(readyState: number) {
    this.readyState = readyState
    this.onerror?.({} as Event)
  }

  close() {
    this.readyState = 2
  }
}

function lastSource() {
  return MockEventSource.instances.at(-1)!
}

beforeEach(() => {
  MockEventSource.instances = []
  vi.stubGlobal('EventSource', MockEventSource)
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('createSseResource', () => {
  it('fetches initial data and opens an EventSource on first subscriber', () => {
    const fetchInitial = vi.fn()
    const res = createSseResource({ streamUrl: '/api/x/stream', fetchInitial, onMessage: vi.fn() })

    res.startStream()

    expect(fetchInitial).toHaveBeenCalledTimes(1)
    expect(MockEventSource.instances).toHaveLength(1)
    expect(lastSource().url).toBe('/api/x/stream')
  })

  it('forwards SSE frames to onMessage', () => {
    const onMessage = vi.fn()
    const res = createSseResource({ streamUrl: '/s', fetchInitial: vi.fn(), onMessage })

    res.startStream()
    lastSource().emit('{"a":1}')

    expect(onMessage).toHaveBeenCalledWith('{"a":1}')
  })

  it('fires onConnected once on the first frame only', () => {
    const onConnected = vi.fn()
    const res = createSseResource({ streamUrl: '/s', fetchInitial: vi.fn(), onMessage: vi.fn(), onConnected })

    res.startStream()
    lastSource().emit('one')
    lastSource().emit('two')

    expect(onConnected).toHaveBeenCalledTimes(1)
  })

  it('falls back to polling on permanent close, then retries SSE after the delay', () => {
    const fetchInitial = vi.fn()
    // Short cadence (< retry delay) so a poll actually fires before the retry preempts it.
    const res = createSseResource({ streamUrl: '/s', fetchInitial, onMessage: vi.fn(), fallbackPollMs: 1_000 })

    res.startStream()
    expect(fetchInitial).toHaveBeenCalledTimes(1) // initial

    lastSource().fail(MockEventSource.CLOSED)

    // Poll lands after the cadence.
    vi.advanceTimersByTime(1_000)
    expect(fetchInitial).toHaveBeenCalledTimes(2)

    // After the retry delay polling stops and a fresh EventSource is created.
    const before = MockEventSource.instances.length
    vi.advanceTimersByTime(SSE_RETRY_DELAY_MS)
    expect(MockEventSource.instances.length).toBe(before + 1)
  })

  it('retry preempts a longer poll cadence (retry delay < fallbackPollMs)', () => {
    const fetchInitial = vi.fn()
    const res = createSseResource({ streamUrl: '/s', fetchInitial, onMessage: vi.fn() }) // default 60s poll, 30s retry

    res.startStream()
    lastSource().fail(MockEventSource.CLOSED)

    // At the retry delay, polling is cleared and SSE reopens before the 60s poll fires.
    const before = MockEventSource.instances.length
    vi.advanceTimersByTime(SSE_RETRY_DELAY_MS)
    expect(MockEventSource.instances.length).toBe(before + 1)
    expect(fetchInitial).toHaveBeenCalledTimes(1) // poll never fired
  })

  it('ignores transient errors (readyState !== CLOSED)', () => {
    const fetchInitial = vi.fn()
    const res = createSseResource({ streamUrl: '/s', fetchInitial, onMessage: vi.fn() })

    res.startStream()
    lastSource().fail(MockEventSource.CONNECTING)

    vi.advanceTimersByTime(SSE_FALLBACK_POLL_MS)
    expect(fetchInitial).toHaveBeenCalledTimes(1) // no polling started
  })

  it('polls immediately when pollLeading is set', () => {
    const fetchInitial = vi.fn()
    const res = createSseResource({ streamUrl: '/s', fetchInitial, onMessage: vi.fn(), pollLeading: true })

    res.startStream()
    lastSource().fail(MockEventSource.CLOSED)

    // initial + leading poll, before any interval elapses
    expect(fetchInitial).toHaveBeenCalledTimes(2)
  })

  it('ref-counts subscribers — stream stays open until the last unsubscribes', () => {
    const fetchInitial = vi.fn()
    const res = createSseResource({ streamUrl: '/s', fetchInitial, onMessage: vi.fn() })

    res.startStream()
    res.startStream() // second subscriber: no new fetch/source
    expect(fetchInitial).toHaveBeenCalledTimes(1)
    expect(MockEventSource.instances).toHaveLength(1)

    res.stopStream()
    expect(lastSource().readyState).not.toBe(2) // still open

    res.stopStream()
    expect(lastSource().readyState).toBe(2) // closed
  })

  it('respects pauseWhenHidden: no SSE while hidden, opens on visibility', () => {
    const docProto = Object.getPrototypeOf(document)
    const hiddenSpy = vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
    const res = createSseResource({
      streamUrl: '/s',
      fetchInitial: vi.fn(),
      onMessage: vi.fn(),
      pauseWhenHidden: true,
    })

    res.startStream()
    expect(MockEventSource.instances).toHaveLength(0) // hidden → no SSE

    hiddenSpy.mockReturnValue(false)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(MockEventSource.instances).toHaveLength(1)

    res.stopStream()
    hiddenSpy.mockRestore()
    void docProto
  })
})
