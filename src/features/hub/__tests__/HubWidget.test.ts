import type { Agent } from '@/types'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { computed, ref } from 'vue'
import { NEEDS_YOU, OPEN_TASK, PENDING_PERMISSIONS } from '@/composables/openTask'

const agents = ref([
  { pid: 101, status: 'active', projectName: 'kontor-hub', working: true },
  { pid: 102, status: 'waiting', projectName: 'web-app', working: false, pendingPermissions: [{}] },
  { pid: 103, status: 'finished', projectName: 'web-app', working: false },
  { pid: 104, status: 'active', projectName: 'api-server', working: false, heldPermissions: [{}] },
  { pid: 105, status: 'idle', projectName: 'worker-queue', working: false },
] as unknown as Agent[])
const ask = vi.fn()

vi.mock('@/features/agents', () => ({
  useAgents: () => ({ agents }),
}))
vi.mock('@/features/mission', () => ({
  NeedsYouQueue: {
    props: ['variant'],
    template: '<div data-testid="stub-queue">{{ variant }}</div>',
  },
  useKontorSession: () => ({ ask }),
  useKontorAgent: () => computed(() => null),
}))

const { default: HubWidget } = await import('../components/HubWidget.vue')

class MockResizeObserver {
  static last: MockResizeObserver | null = null
  callback: ResizeObserverCallback
  observe = vi.fn()
  unobserve = vi.fn()
  disconnect = vi.fn()
  constructor(callback: ResizeObserverCallback) {
    this.callback = callback
    MockResizeObserver.last = this
  }
}

beforeEach(() => {
  vi.stubGlobal('ResizeObserver', MockResizeObserver)
  ask.mockClear()
})

async function mountHub() {
  const w = mount(HubWidget, {
    attachTo: document.body,
    global: {
      provide: {
        [NEEDS_YOU]: computed(() => []),
        [PENDING_PERMISSIONS]: { items: ref([]), refresh: vi.fn() },
        [OPEN_TASK]: vi.fn(),
      },
    },
  })
  // vueuse's post-flush observers bind to the stage only after the next tick.
  await flushPromises()
  const observer = MockResizeObserver.last!
  observer.callback([{ contentRect: { width: 1090, height: 1130 } } as ResizeObserverEntry], observer as unknown as ResizeObserver)
  await flushPromises()
  return w
}

// The core sits at the centre of the 1090×1130 stage; at fit one world unit is one pixel.
function distanceFromCore(w: Awaited<ReturnType<typeof mountHub>>, pid: number): number {
  const [, x, y] = /translate\(([-\d.]+)px, ([-\d.]+)px/.exec(w.get(`[data-testid="hub-agent-${pid}"]`).attributes('style')!)!
  return Math.hypot(Number(x) - 545, Number(y) - 565)
}

describe('hubWidget', () => {
  it('draws the stage at the overview level with one button per live agent and the docked queue', async () => {
    const w = await mountHub()
    expect(w.get('[data-testid="hub-stage"]').attributes('data-level')).toBe('0')
    expect(w.find('[data-testid="hub-agent-101"]').exists()).toBe(true)
    expect(w.find('[data-testid="hub-agent-102"]').exists()).toBe(true)
    expect(w.find('[data-testid="hub-agent-103"]').exists()).toBe(false)
    expect(w.get('[data-testid="hub-agent-102"]').attributes('aria-label')).toContain('needs you')
    expect(w.get('[data-testid="hub-core"]').text()).toContain('2 running · 2 need you')
    expect(w.get('[data-testid="stub-queue"]').text()).toBe('docked')
    w.unmount()
  })

  it('places a needs-you agent closer to the core than a working one', async () => {
    const w = await mountHub()
    const dist = (pid: number) => distanceFromCore(w, pid)
    expect(dist(102)).toBeCloseTo(88)
    expect(dist(101)).toBeCloseTo(116)
    w.unmount()
  })

  it('rings a held permission as needing the operator, but not an agent that merely has its turn', async () => {
    const w = await mountHub()
    const dist = (pid: number) => distanceFromCore(w, pid)
    const dot = (pid: number) => w.get(`[data-testid="hub-agent-${pid}"] span`).classes()

    expect(w.get('[data-testid="hub-agent-104"]').attributes('aria-label')).toBe('Api Server, Active, needs you')
    expect(dot(104)).toEqual(expect.arrayContaining(['outline-warning', 'motion-safe:animate-pulse']))
    expect(dist(104)).toBeCloseTo(88)

    expect(w.get('[data-testid="hub-agent-105"]').attributes('aria-label')).toBe('Worker Queue, Idle')
    expect(dot(105)).not.toContain('outline-warning')
    expect(dist(105)).toBeCloseTo(116)
    w.unmount()
  })

  it('opens the Kontor session when the core is pressed', async () => {
    const w = await mountHub()
    await w.get('[data-testid="hub-core"]').trigger('click')
    expect(ask).toHaveBeenCalledOnce()
    w.unmount()
  })
})
