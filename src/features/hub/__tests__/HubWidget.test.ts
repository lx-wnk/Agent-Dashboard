import type { GraphStatus, HubNote } from '../composables/useObsidianGraph'
import type { Agent } from '@/types'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { computed, ref, shallowRef } from 'vue'
import { NEEDS_YOU, OPEN_SETTINGS, OPEN_TASK, PENDING_PERMISSIONS } from '@/composables/openTask'
import { useSidebar } from '@/composables/useSidebar'
import { useViewState } from '@/composables/useViewState'
import { DEFAULT_LAYOUT, useWorkspace } from '@/features/workspace'
import HubBrainCanvas from '../components/HubBrainCanvas.vue'
import { hubFocusRequest } from '../composables/useHubFocus'
import { fitScale } from '../hubCamera'
import { AGENT_SPACING_PX, AGENT_STAGE_MARGIN_PX, DAY_MS, notePoint, planSectors } from '../hubGeometry'

const NOTE_AGE_DAYS = 30
const graph = {
  status: ref<GraphStatus>('idle'),
  message: ref(''),
  notes: shallowRef<HubNote[]>([]),
  refresh: vi.fn(async () => {}),
  recentNotes: (count: number) => [...graph.notes.value].sort((a, b) => b.mtimeMs - a.mtimeMs).slice(0, count),
  noteByPath: (path: string) => graph.notes.value.find(n => n.path === path),
  openInObsidian: vi.fn(async () => null),
}
const openSettings = vi.fn()

vi.mock('../composables/useObsidianGraph', () => ({ useObsidianGraph: () => graph }))

function vaultNote(index: number, path: string): HubNote {
  return { index, path, title: path, mtimeMs: Date.now() - NOTE_AGE_DAYS * DAY_MS, links: [], backlinks: [] }
}

const agents = ref([
  { pid: 101, status: 'active', projectName: 'kontor-hub', working: true },
  { pid: 102, status: 'waiting', projectName: 'web-app', working: false, pendingPermissions: [{}] },
  { pid: 103, status: 'finished', projectName: 'web-app', working: false },
  { pid: 104, status: 'active', projectName: 'api-server', working: false, heldPermissions: [{}] },
  { pid: 105, status: 'idle', projectName: 'worker-queue', working: false },
] as unknown as Agent[])
const initialAgents = agents.value
const ask = vi.fn()
const overlayOpen = ref(false)

vi.mock('@/features/agents', () => ({
  useAgents: () => ({ agents, selectAgent: vi.fn() }),
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
  // jsdom has no canvas; the brain layer only needs a context that accepts every call.
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(new Proxy({}, { get: () => () => {}, set: () => true }) as never)
})

afterEach(() => {
  graph.status.value = 'idle'
  graph.message.value = ''
  graph.notes.value = []
  graph.refresh.mockClear()
  openSettings.mockClear()
  agents.value = initialAgents
  useWorkspace().layout.value = DEFAULT_LAYOUT
  useWorkspace().wide.value = null
  hubFocusRequest.value = null
})

async function mountHub() {
  const w = mount(HubWidget, {
    attachTo: document.body,
    global: {
      provide: {
        [NEEDS_YOU]: computed(() => []),
        [PENDING_PERMISSIONS]: { items: ref([]), refresh: vi.fn() },
        [OPEN_TASK]: vi.fn(),
        [OPEN_SETTINGS]: openSettings,
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

// A real key lands on whatever holds focus and bubbles from there.
async function pressFocused(key: string): Promise<KeyboardEvent> {
  const e = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true })
  document.activeElement!.dispatchEvent(e)
  await flushPromises()
  return e
}

const LIST = '[role="dialog"][aria-label="Zentrale as a list"]'

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

  it('widens the agent ring with the agent count so labels get room', async () => {
    agents.value = Array.from({ length: 12 }, (_, i) => ({ pid: 200 + i, status: 'idle', projectName: `project-${i}`, working: false })) as unknown as Agent[]
    const w = await mountHub()
    expect(distanceFromCore(w, 200)).toBeCloseTo(12 * AGENT_SPACING_PX / (2 * Math.PI))
    w.unmount()
  })

  it('caps the agent ring by the stage so a crowd stays inside it', async () => {
    agents.value = Array.from({ length: 40 }, (_, i) => ({ pid: 200 + i, status: 'idle', projectName: `project-${i}`, working: false })) as unknown as Agent[]
    const w = await mountHub()
    expect(distanceFromCore(w, 200)).toBeCloseTo(1090 / 2 - AGENT_STAGE_MARGIN_PX)
    w.unmount()
  })

  it('docks the launchers when their ring would not clear the agents, and keeps the ring when it does', async () => {
    const launcherX = (w: Awaited<ReturnType<typeof mountHub>>) => Number(/translate\(([-\d.]+)px/.exec(w.get('[data-testid^="hub-launcher-"]').attributes('style')!)![1])
    const roomy = await mountHub()
    expect(launcherX(roomy)).not.toBe(30)
    roomy.unmount()

    agents.value = Array.from({ length: 40 }, (_, i) => ({ pid: 200 + i, status: 'idle', projectName: `project-${i}`, working: false })) as unknown as Agent[]
    const crowded = await mountHub()
    expect(launcherX(crowded)).toBe(30)
    crowded.unmount()
  })

  it('draws no sector names without notes, where every sector is one agent\'s project', async () => {
    const w = await mountHub()
    expect(w.findAll('[data-testid^="hub-sector-"]')).toHaveLength(0)
    w.unmount()
  })

  it('keeps a minimum height while tiles stack in one column, and fills its tile from md up', async () => {
    const w = await mountHub()
    expect(w.get('[data-testid="hub"]').classes()).toEqual(expect.arrayContaining(['min-h-[34rem]', 'md:min-h-0']))
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

  it('closes the focused list on Escape before it fits', async () => {
    const w = await mountHub()
    await press(w, '+')
    await press(w, 'L')
    expect(w.get(LIST).element.contains(document.activeElement)).toBe(true)
    await pressFocused('Escape')
    expect(w.find(LIST).exists()).toBe(false)
    expect(scale(w)).toBeCloseTo(1.4)
    await pressFocused('Escape')
    expect(scale(w)).toBeCloseTo(1)
    w.unmount()
  })

  it('flies to an agent picked in the orbit and opens its card; Escape from the focused list closes the card, then the list, then fits', async () => {
    const w = await mountHub()
    const stage = w.get('[data-testid="hub-stage"]').element
    await w.get('[data-testid="hub-agent-101"]').trigger('click')
    expect(scale(w)).toBeCloseTo(3)
    expect(w.get('[role="dialog"][aria-label="Kontor Hub"]').text()).toContain('Working')
    await press(w, 'L')
    expect(document.activeElement).toBe(w.get(`${LIST} button`).element)
    await pressFocused('Escape')
    expect(w.find('[aria-label="Kontor Hub"]').exists()).toBe(false)
    expect(w.find(LIST).exists()).toBe(true)
    await pressFocused('Escape')
    expect(w.find(LIST).exists()).toBe(false)
    expect(document.activeElement).toBe(stage)
    expect(scale(w)).toBeCloseTo(3)
    await pressFocused('Escape')
    expect(scale(w)).toBeCloseTo(1)
    w.unmount()
  })

  it('closes the card on Escape pressed inside it and hands focus back to the stage', async () => {
    const w = await mountHub()
    await w.get('[data-testid="hub-agent-101"]').trigger('click')
    const open = w.findAll('[aria-label="Kontor Hub"] button').find(b => b.text() === 'Open session')!
    ;(open.element as HTMLElement).focus()
    await pressFocused('Escape')
    expect(w.find('[aria-label="Kontor Hub"]').exists()).toBe(false)
    expect(document.activeElement).toBe(w.get('[data-testid="hub-stage"]').element)
    expect(scale(w)).toBeCloseTo(3)
    w.unmount()
  })

  it('closes the list on an agent picked in it, flies there and opens its card', async () => {
    const w = await mountHub()
    await press(w, 'L')
    const row = w.findAll('[data-testid="hub-list-agent"]').find(r => r.attributes('aria-label') === 'Web App, Quiet')!
    await row.trigger('click')
    expect(w.find('[aria-label="Zentrale as a list"]').exists()).toBe(false)
    expect(w.find('[role="dialog"][aria-label="Web App"]').exists()).toBe(true)
    expect(scale(w)).toBeCloseTo(3)
    w.unmount()
  })

  it('flies to the point picked on the minimap', async () => {
    const w = await mountHub()
    const map = w.get('svg[aria-label="Overview map"]')
    map.element.getBoundingClientRect = () => ({ left: 0, top: 0, width: 108, height: 108 }) as DOMRect
    await map.trigger('click', { clientX: 81, clientY: 54 })
    expect(scale(w)).toBeCloseTo(2)
    expect(w.get('svg g').attributes('transform')).toBe('translate(5,565) scale(2)')
    w.unmount()
  })

  it('leaves Escape from the focused list to the open Kontor overlay above it', async () => {
    const w = await mountHub()
    await press(w, '+')
    await press(w, 'L')
    overlayOpen.value = true
    const escape = await pressFocused('Escape')
    expect(escape.defaultPrevented).toBe(false)
    expect(w.find(LIST).exists()).toBe(true)
    overlayOpen.value = false
    await pressFocused('Escape')
    expect(w.find(LIST).exists()).toBe(false)
    expect(scale(w)).toBeCloseTo(1.4)
    await pressFocused('Escape')
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

  it('refreshes the vault graph on mount and when the window regains focus', async () => {
    const w = await mountHub()
    expect(graph.refresh).toHaveBeenCalledOnce()
    window.dispatchEvent(new Event('focus'))
    expect(graph.refresh).toHaveBeenCalledTimes(2)
    w.unmount()
  })

  it('names a sector per top-level vault folder once the graph is ready', async () => {
    graph.status.value = 'ready'
    graph.notes.value = [vaultNote(0, 'alpha/one.md'), vaultNote(1, 'beta/two.md')]
    const w = await mountHub()
    const names = w.findAll('[data-testid^="hub-sector-"]').map(b => b.text()).join(' ')
    expect(names).toContain('alpha')
    expect(names).toContain('beta')
    expect(w.find('canvas[aria-hidden="true"]').exists()).toBe(true)
    w.unmount()
  })

  it('keeps the sectors and the notes on screen while the graph refetches', async () => {
    graph.status.value = 'ready'
    graph.notes.value = [vaultNote(0, 'alpha/one.md'), vaultNote(1, 'beta/two.md')]
    const w = await mountHub()
    const sectorNames = () => w.findAll('[data-testid^="hub-sector-"]').map(b => b.text())
    const namesBefore = sectorNames()
    const pointsBefore = w.getComponent(HubBrainCanvas).props('points')
    expect(pointsBefore).toHaveLength(2)

    graph.status.value = 'loading'
    await flushPromises()
    expect(sectorNames()).toEqual(namesBefore)
    expect(w.getComponent(HubBrainCanvas).props('points')).toEqual(pointsBefore)
    w.unmount()
  })

  it('flies to a note tapped at the overview level', async () => {
    graph.status.value = 'ready'
    graph.notes.value = [vaultNote(0, 'alpha/one.md'), vaultNote(1, 'beta/two.md')]
    const w = await mountHub()
    const { sectors, sectorOfNote } = planSectors(['alpha/one.md', 'beta/two.md'], ['kontor-hub', 'web-app', 'api-server', 'worker-queue'])
    const [x, y] = notePoint('alpha/one.md', sectors.find(s => s.key === sectorOfNote.get('alpha/one.md'))!, NOTE_AGE_DAYS)
    const stage = w.get('[data-testid="hub-stage"]').element
    for (const type of ['pointerdown', 'pointerup'])
      stage.dispatchEvent(new PointerEvent(type, { bubbles: true, pointerId: 1, clientX: 545 + x, clientY: 565 + y }))
    await flushPromises()
    expect(scale(w)).toBeCloseTo(2.6)
    w.unmount()
  })

  it('offers to connect Obsidian while the vault is unconfigured', async () => {
    graph.status.value = 'unconfigured'
    const w = await mountHub()
    const notice = w.get('[data-testid="hub-graph-notice"]')
    expect(notice.text()).toContain('Connect Obsidian to see your notes here.')
    await notice.get('button').trigger('click')
    expect(openSettings).toHaveBeenCalledOnce()
    w.unmount()
  })

  it('explains a denied or failed vault read, and says nothing while loading', async () => {
    graph.status.value = 'denied'
    graph.message.value = 'memory.read denied'
    const w = await mountHub()
    const notice = w.get('[data-testid="hub-graph-notice"]')
    expect(notice.text()).toBe('Memory reads are not granted, so your notes stay hidden.')
    expect(notice.attributes('title')).toBe('memory.read denied')

    graph.status.value = 'failed'
    await flushPromises()
    expect(w.get('[data-testid="hub-graph-notice"]').text()).toBe('Your notes could not be loaded; retrying when you come back to this window.')

    graph.status.value = 'loading'
    await flushPromises()
    expect(w.find('[data-testid="hub-graph-notice"]').exists()).toBe(false)
    w.unmount()
  })

  it('lists the recently touched notes by sector, and a row closes the list, flies to the note and opens its card', async () => {
    graph.status.value = 'ready'
    graph.notes.value = [vaultNote(0, 'alpha/one.md'), vaultNote(1, 'beta/two.md')]
    const w = await mountHub()
    await press(w, 'L')
    const rows = w.findAll('[data-testid="hub-list-note"]')
    expect(rows.map(r => r.attributes('aria-label'))).toEqual([expect.stringMatching(/^alpha\/one\.md, alpha, /), expect.stringMatching(/^beta\/two\.md, beta, /)])
    await rows[1].trigger('click')
    expect(w.find(LIST).exists()).toBe(false)
    expect(scale(w)).toBeCloseTo(5)
    expect(w.get('[role="dialog"][aria-label="beta/two.md"]').text()).toContain('beta')
    expect(w.getComponent(HubBrainCanvas).props('selected')).toBe(1)
    w.unmount()
  })

  it('flies from a link chip to the linked note at least at rel 3 and opens its card; Escape closes the card first', async () => {
    graph.status.value = 'ready'
    graph.notes.value = [{ ...vaultNote(0, 'alpha/one.md'), links: [1] }, { ...vaultNote(1, 'beta/two.md'), backlinks: [0] }]
    const w = await mountHub()
    await press(w, 'L')
    await w.findAll('[data-testid="hub-list-note"]')[0].trigger('click')
    await w.get('[data-testid="hub-note-link"]').trigger('click')
    expect(scale(w)).toBeCloseTo(5)
    expect(w.find('[role="dialog"][aria-label="beta/two.md"]').exists()).toBe(true)
    expect(w.find('[role="dialog"][aria-label="alpha/one.md"]').exists()).toBe(false)
    await pressFocused('Escape')
    expect(w.find('[role="dialog"][aria-label="beta/two.md"]').exists()).toBe(false)
    expect(w.getComponent(HubBrainCanvas).props('selected')).toBeNull()
    expect(scale(w)).toBeCloseTo(5)
    w.unmount()
  })

  it('keeps the selected note by path when a refetch shifts the indices', async () => {
    graph.status.value = 'ready'
    graph.notes.value = [vaultNote(0, 'alpha/one.md'), vaultNote(1, 'beta/two.md')]
    const w = await mountHub()
    await press(w, 'L')
    await w.findAll('[data-testid="hub-list-note"]')[1].trigger('click')
    graph.notes.value = [vaultNote(0, 'alpha/new.md'), vaultNote(1, 'alpha/one.md'), vaultNote(2, 'beta/two.md')]
    await flushPromises()
    expect(w.getComponent(HubBrainCanvas).props('selected')).toBe(2)
    expect(w.find('[role="dialog"][aria-label="beta/two.md"]').exists()).toBe(true)
    w.unmount()
  })

  it('asks Kontor about the open note as a wiki link', async () => {
    graph.status.value = 'ready'
    graph.notes.value = [vaultNote(0, 'alpha/one.md')]
    const w = await mountHub()
    await press(w, 'L')
    await w.get('[data-testid="hub-list-note"]').trigger('click')
    await w.findAll('button').find(b => b.text() === 'Ask Kontor about this')!.trigger('click')
    expect(ask).toHaveBeenCalledWith('[[alpha/one]] ')
    w.unmount()
  })

  it('flies to a note focus request at rel 5, opens its card and clears the request', async () => {
    graph.status.value = 'ready'
    graph.notes.value = [vaultNote(0, 'alpha/one.md'), vaultNote(1, 'beta/two.md')]
    const w = await mountHub()
    hubFocusRequest.value = { kind: 'note', path: 'beta/two.md' }
    await flushPromises()
    expect(scale(w)).toBeCloseTo(5)
    expect(w.get('[role="dialog"][aria-label="beta/two.md"]').text()).toContain('beta')
    expect(hubFocusRequest.value).toBeNull()
    w.unmount()
  })

  it('flies to an agent focus request at rel 3, opens its card and clears the request', async () => {
    const w = await mountHub()
    hubFocusRequest.value = { kind: 'agent', pid: 102 }
    await flushPromises()
    expect(scale(w)).toBeCloseTo(3)
    expect(w.get('[role="dialog"][aria-label="Web App"]')).toBeTruthy()
    expect(hubFocusRequest.value).toBeNull()
    w.unmount()
  })

  it('clears an unresolved focus request without flying or opening a card', async () => {
    const w = await mountHub()
    hubFocusRequest.value = { kind: 'agent', pid: 9999 }
    await flushPromises()
    expect(scale(w)).toBeCloseTo(1)
    expect(w.find('[role="dialog"]').exists()).toBe(false)
    expect(hubFocusRequest.value).toBeNull()
    w.unmount()
  })

  it('consumes a focus request already pending when the widget mounts', async () => {
    graph.status.value = 'ready'
    graph.notes.value = [vaultNote(0, 'alpha/one.md')]
    hubFocusRequest.value = { kind: 'note', path: 'alpha/one.md' }
    const w = await mountHub()
    expect(w.get('[role="dialog"][aria-label="alpha/one.md"]')).toBeTruthy()
    expect(scale(w) / fitScale(1090, 1130)).toBeCloseTo(5)
    expect(hubFocusRequest.value).toBeNull()
    w.unmount()
  })
})
