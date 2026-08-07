import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import { openListbox, optionByLabel } from '@/utils/testSelect'
import DashboardToolbar from './DashboardToolbar.vue'

// AppSelect (used for project/spawner/sort/group filters) is a custom listbox,
// not a native <select> — its panel teleports to <body> while open, so option
// counts and value changes are exercised through the trigger button + the
// teleported [role="option"] elements instead of select.findAll('option') /
// select.setValue(), which only work against native <select> internals. See
// testSelect.ts for the shared openListbox()/optionByLabel() helpers.

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

  it('offers "Group by spawner" while no spawner is filtered', async () => {
    const w = mount(DashboardToolbar, { props: BASE_PROPS, attachTo: document.body })
    const panel = await openListbox(w.get('[data-testid="select-group"]'))
    expect(panel.querySelectorAll('[role="option"]')).toHaveLength(5)
    expect(optionByLabel(panel, 'Group by spawner')).toBeTruthy()
  })

  it('hides "Group by spawner" once a spawner is filtered', async () => {
    const props = { ...BASE_PROPS, spawner: 'claude' }
    const w = mount(DashboardToolbar, { props, attachTo: document.body })
    const panel = await openListbox(w.get('[data-testid="select-group"]'))
    const labels = [...panel.querySelectorAll('[role="option"]')].map(o => o.textContent?.trim())
    expect(labels).toHaveLength(4)
    expect(labels).not.toContain('Group by spawner')
  })

  it('shows "No grouping" when a spawner filter hides the stored spawner grouping', () => {
    const props = { ...BASE_PROPS, spawner: 'claude', groupBy: 'spawner' as const }
    const w = mount(DashboardToolbar, { props, attachTo: document.body })
    expect(w.get('[data-testid="select-group"]').text()).toContain('No grouping')
  })
})
