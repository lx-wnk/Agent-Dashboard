import type { Agent } from '@/types'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { computed, ref } from 'vue'
import { NEEDS_YOU, OPEN_TASK, PENDING_PERMISSIONS } from '@/composables/openTask'
import { useSidebar } from '@/composables/useSidebar'
import { useViewState } from '@/composables/useViewState'
import { DEFAULT_LAYOUT, useWorkspace } from '@/features/workspace'

const agents = ref([
  { pid: 101, status: 'active', projectName: 'kontor-hub', working: true },
  { pid: 102, status: 'waiting', projectName: 'web-app', working: false, pendingPermissions: [{}] },
  { pid: 103, status: 'finished', projectName: 'web-app', working: false },
  { pid: 104, status: 'active', projectName: 'api-server', working: false, heldPermissions: [{}] },
  { pid: 105, status: 'idle', projectName: 'worker-queue', working: false },
] as unknown as Agent[])
const ask = vi.fn()
const overlayOpen = ref(false)

vi.mock('@/features/agents', () => ({
  useAgents: () => ({ agents }),
}))
vi.mock('@/features/mission', () => ({
  NeedsYouQueue: {
    props: ['variant'],
    template: '<div data-testid="stub-queue">{{ variant }}</div>',
  },
  useKontorSession: () => ({ ask, overlayOpen }),
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
  window.matchMedia = vi.fn((query: string) => ({
    matches: query.includes('reduce'),
    media: query,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  })) as unknown as typeof window.matchMedia
  ask.mockClear()
  overlayOpen.value = false
  useViewState().activeView.value = 'zentrale'
})

afterEach(() => {
  useWorkspace().layout.value = DEFAULT_LAYOUT
  useWorkspace().wide.value = null
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

function scale(w: Awaited<ReturnType<typeof mountHub>>): number {
  return Number(/scale\(([-\d.]+)\)/.exec(w.get('svg g').attributes('transform')!)![1])
}

async function press(w: Awaited<ReturnType<typeof mountHub>>, key: string) {
  await w.get('[data-testid="hub-stage"]').trigger('keydown', { key })
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

  it('zooms with + and returns to the overview with 0', async () => {
    const w = await mountHub()
    await press(w, '+')
    expect(scale(w)).toBeCloseTo(1.4)
    await press(w, '0')
    expect(scale(w)).toBeCloseTo(1)
    w.unmount()
  })

  it('ignores keys typed into an input inside the stage and keys held with a modifier', async () => {
    const w = await mountHub()
    const input = document.createElement('input')
    w.get('[data-testid="hub-stage"]').element.appendChild(input)
    input.dispatchEvent(new KeyboardEvent('keydown', { key: '+', bubbles: true }))
    await press(w, '-')
    await w.get('[data-testid="hub-stage"]').trigger('keydown', { key: '+', ctrlKey: true })
    expect(scale(w)).toBeCloseTo(1 / 1.4)
    w.unmount()
  })

  it('toggles the hub wide with F', async () => {
    const w = await mountHub()
    await press(w, 'F')
    expect(useWorkspace().wide.value).toBe('hub')
    expect(w.get('button[aria-label="Widen"]').attributes('aria-pressed')).toBe('true')
    await press(w, 'f')
    expect(useWorkspace().wide.value).toBeNull()
    w.unmount()
  })

  it('offers every other view and a new page as launchers, and fires the slot a digit names', async () => {
    const w = await mountHub()
    const ids = w.findAll('[data-testid^="hub-launcher-"]').map(b => b.attributes('data-testid'))
    expect(ids).toEqual(['dashboard', 'pipeline', 'schedules', 'workflows', 'cost', 'eval', 'new-page'].map(id => `hub-launcher-${id}`))
    await press(w, '2')
    expect(useViewState().activeView.value).toBe('pipeline')
    w.unmount()
  })

  it('asks the sidebar for a new page from the new-page launcher', async () => {
    const w = await mountHub()
    const before = useSidebar().newPageRequests.value
    await w.get('[data-testid="hub-launcher-new-page"]').trigger('click')
    expect(useSidebar().newPageRequests.value).toBe(before + 1)
    w.unmount()
  })

  it('closes the list on Escape before it fits', async () => {
    const w = await mountHub()
    await press(w, '+')
    await press(w, 'L')
    await press(w, 'Escape')
    expect(scale(w)).toBeCloseTo(1.4)
    await press(w, 'Escape')
    expect(scale(w)).toBeCloseTo(1)
    w.unmount()
  })

  it('leaves Escape to the open Kontor overlay above it', async () => {
    const w = await mountHub()
    await press(w, '+')
    await press(w, 'L')
    overlayOpen.value = true
    const escape = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
    w.get('[data-testid="hub-stage"]').element.dispatchEvent(escape)
    expect(escape.defaultPrevented).toBe(false)
    overlayOpen.value = false
    await press(w, 'Escape')
    expect(scale(w)).toBeCloseTo(1.4)
    await press(w, 'Escape')
    expect(scale(w)).toBeCloseTo(1)
    w.unmount()
  })

  it('flies to the picked level', async () => {
    const w = await mountHub()
    await w.findAll('button').find(b => b.text() === 'Notes')!.trigger('click')
    expect(scale(w)).toBeCloseTo(5.5)
    expect(w.get('[data-testid="hub-stage"]').attributes('data-level')).toBe('2')
    w.unmount()
  })

  it('opens the page holding the Kontor tile before asking, from a page without one', async () => {
    useWorkspace().layout.value = { version: 1, pages: [...DEFAULT_LAYOUT.pages, { id: 'p-a', title: 'Morning', tiles: [] }] }
    useViewState().activeView.value = 'page:p-a'
    const w = await mountHub()
    await w.get('[data-testid="hub-core"]').trigger('click')
    expect(useViewState().activeView.value).toBe('zentrale')
    expect(ask).toHaveBeenCalledOnce()
    w.unmount()
  })

  it('does nothing on the core when no page holds the Kontor tile', async () => {
    useWorkspace().layout.value = { version: 1, pages: [{ id: 'zentrale', title: 'Zentrale', tiles: [] }] }
    const w = await mountHub()
    const core = w.get('[data-testid="hub-core"]')
    expect(core.attributes('title')).toBe('Add the Kontor tile to a page to open it here')
    await core.trigger('click')
    expect(ask).not.toHaveBeenCalled()
    w.unmount()
  })
})
