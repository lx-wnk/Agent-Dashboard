import { enableAutoUnmount } from '@vue/test-utils'
import { afterEach } from 'vitest'

// Global Vitest setup.
//
// Several composables persist state or reconnect via fire-and-forget macrotasks
// (deferred localStorage writes, SSE EventSource reconnects). A callback
// scheduled while one test file runs can fire slightly later, when the global it
// references is momentarily absent — surfacing as an unhandled TypeError /
// ReferenceError that fails an unrelated, later test and makes the suite order-
// dependent. Providing inert fallbacks lets such an orphaned callback no-op
// instead of throwing. Individual tests still install their own richer stubs.

// A mounted component keeps its own timers and observers running, and Vue Test
// Utils never tears wrappers down on its own. A test that forgets unmount()
// therefore leaves a live poller behind that keeps writing to the module-level
// mocks the following tests share, inflating their call counts once enough real
// time passes for a tick to land (#327). Unmounting after every test keeps a
// missing unmount() from crossing a test boundary.
enableAutoUnmount(afterEach)

class InertEventSource {
  static readonly CONNECTING = 0
  static readonly OPEN = 1
  static readonly CLOSED = 2
  readyState = InertEventSource.CLOSED
  onmessage: ((e: MessageEvent) => void) | null = null
  onerror: ((e: Event) => void) | null = null
  onopen: ((e: Event) => void) | null = null
  close(): void {}
  addEventListener(): void {}
  removeEventListener(): void {}
}

if (typeof globalThis.EventSource === 'undefined')
  globalThis.EventSource = InertEventSource as unknown as typeof EventSource

// jsdom does not implement requestIdleCallback; fall back to a macrotask so the
// browser-preferred code path behaves identically under test.
globalThis.requestIdleCallback ??= ((cb: IdleRequestCallback): number =>
  setTimeout(() => cb({ didTimeout: false, timeRemaining: () => 0 }), 0) as unknown as number) as typeof requestIdleCallback
globalThis.cancelIdleCallback ??= ((id: number): void => clearTimeout(id)) as typeof cancelIdleCallback
