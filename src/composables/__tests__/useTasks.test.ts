import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'

// Minimal EventSource stub to prevent actual network calls.
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

beforeEach(() => {
  MockEventSource.instances = []
  vi.stubGlobal('EventSource', MockEventSource)
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: true,
    json: () => Promise.resolve([]),
  }))
})

afterEach(() => {
  vi.unstubAllGlobals()
})

/**
 * Helper: mount a wrapper component to provide the Vue lifecycle context
 * that composables using onUnmounted require.
 */
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

describe('useTasks', () => {
  it('initialises with empty task list', async () => {
    const { useTasks } = await import('../useTasks')
    const { result, wrapper } = withSetup(() => useTasks({ autoStart: false }))
    expect(result.tasks).toBeDefined()
    expect(Array.isArray(result.tasks.value)).toBe(true)
    wrapper.unmount()
  })

  it('exposes a loading ref that starts as true', async () => {
    const { useTasks } = await import('../useTasks')
    const { result, wrapper } = withSetup(() => useTasks({ autoStart: false }))
    expect(result.isLoading.value).toBe(true)
    wrapper.unmount()
  })

  it('exposes selectTask function that updates selectedTask', async () => {
    const { useTasks } = await import('../useTasks')
    const { result, wrapper } = withSetup(() => useTasks({ autoStart: false }))
    expect(typeof result.selectTask).toBe('function')
    // Set a non-null task first, then clear it to verify null assignment works.
    // Note: Vue wraps objects in a Proxy, so use toStrictEqual rather than toBe.
    const fakeTask = { id: 'test-id' } as any
    result.selectTask(fakeTask)
    expect(result.selectedTask.value).toStrictEqual(fakeTask)
    result.selectTask(null)
    expect(result.selectedTask.value).toBeNull()
    wrapper.unmount()
  })

  it('exposes tasksByStage function', async () => {
    const { useTasks } = await import('../useTasks')
    const { result, wrapper } = withSetup(() => useTasks({ autoStart: false }))
    expect(typeof result.tasksByStage).toBe('function')
    wrapper.unmount()
  })
})
