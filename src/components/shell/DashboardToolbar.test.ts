import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import DashboardToolbar from './DashboardToolbar.vue'

// AppSelect (used for project/spawner/sort/group filters) is a custom listbox,
// not a native <select> — its panel teleports to <body> while open, so option
// counts and value changes are exercised through the trigger button + the
// teleported [role="option"] elements instead of select.findAll('option') /
// select.setValue(), which only work against native <select> internals.
interface SelectTrigger {
  trigger: (event: string) => Promise<unknown>
  attributes: (key: string) => string | undefined
}

async function openListbox(select: SelectTrigger): Promise<HTMLElement> {
  await select.trigger('click')
  return document.getElementById(select.attributes('aria-controls')!)!
}

function optionByLabel(panel: HTMLElement, label: string): HTMLElement {
  const match = Array.from(panel.querySelectorAll('[role="option"]'))
    .find(el => el.textContent?.trim() === label)
  if (!match)
    throw new Error(`No option with label "${label}" found`)
  return match as HTMLElement
}

afterEach(() => {
  document.body.innerHTML = ''
})

const BASE_PROPS = {
  layout: 'cards' as const,
  spawner: 'all',
  project: 'all',
  sortBy: 'latest' as const,
  groupBy: 'none' as const,
  projectOptions: [{ value: 'all', label: 'All projects' }],
  spawnerOptions: [
    { value: 'all', label: 'All spawners' },
    { value: 'claude', label: 'Claude Code' },
  ],
}

describe('dashboardToolbar', () => {
  it('marks the active layout button aria-pressed', () => {
    const w = mount(DashboardToolbar, { props: BASE_PROPS })
    const cards = w.get('[data-testid="layout-cards"]')
    expect(cards.attributes('aria-pressed')).toBe('true')
  })

  it('emits update:layout when clicking List', async () => {
    const w = mount(DashboardToolbar, { props: BASE_PROPS })
    await w.get('[data-testid="layout-list"]').trigger('click')
    expect(w.emitted('update:layout')![0]).toEqual(['list'])
  })

  it('renders project select with provided options', async () => {
    const props = {
      ...BASE_PROPS,
      projectOptions: [
        { value: 'all', label: 'All projects' },
        { value: 'my-project', label: 'my-project' },
      ],
    }
    const w = mount(DashboardToolbar, { props, attachTo: document.body })
    const panel = await openListbox(w.get('[data-testid="select-project"]'))
    expect(panel.querySelectorAll('[role="option"]')).toHaveLength(2)
  })

  it('emits update:project when project select changes', async () => {
    const props = {
      ...BASE_PROPS,
      projectOptions: [
        { value: 'all', label: 'All projects' },
        { value: 'my-project', label: 'my-project' },
      ],
    }
    const w = mount(DashboardToolbar, { props, attachTo: document.body })
    const panel = await openListbox(w.get('[data-testid="select-project"]'))
    optionByLabel(panel, 'my-project').dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await w.vm.$nextTick()
    expect(w.emitted('update:project')?.[0]).toEqual(['my-project'])
  })

  it('renders spawner select with provided options', async () => {
    const w = mount(DashboardToolbar, { props: BASE_PROPS, attachTo: document.body })
    const panel = await openListbox(w.get('[data-testid="select-spawner"]'))
    expect(panel.querySelectorAll('[role="option"]')).toHaveLength(2)
  })

  it('emits update:spawner when spawner select changes', async () => {
    const w = mount(DashboardToolbar, { props: BASE_PROPS, attachTo: document.body })
    const panel = await openListbox(w.get('[data-testid="select-spawner"]'))
    optionByLabel(panel, 'Claude Code').dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await w.vm.$nextTick()
    expect(w.emitted('update:spawner')?.[0]).toEqual(['claude'])
  })

  it('emits update:sortBy when sort select changes', async () => {
    const w = mount(DashboardToolbar, { props: BASE_PROPS, attachTo: document.body })
    const panel = await openListbox(w.get('[data-testid="select-sort"]'))
    optionByLabel(panel, 'Longest running').dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await w.vm.$nextTick()
    expect(w.emitted('update:sortBy')?.[0]).toEqual(['longest'])
  })

  it('emits update:groupBy when group select changes', async () => {
    const w = mount(DashboardToolbar, { props: BASE_PROPS, attachTo: document.body })
    const panel = await openListbox(w.get('[data-testid="select-group"]'))
    optionByLabel(panel, 'Group by status').dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await w.vm.$nextTick()
    expect(w.emitted('update:groupBy')?.[0]).toEqual(['status'])
  })
})
