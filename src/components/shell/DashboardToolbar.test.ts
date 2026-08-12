import type { AgentGroup, AgentSort } from '@/utils/agentGroup'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import { nextTick } from 'vue'
import { openListbox, optionByLabel } from '@/utils/testSelect'
import DashboardToolbar from './DashboardToolbar.vue'

// AppSelect (used for the sort/group controls and the two filter selects inside
// the filter menu) is a custom listbox, not a native <select> — its panel
// teleports to <body> while open, so option counts and value changes are
// exercised through the trigger button + the teleported [role="option"] elements
// instead of select.findAll('option') / select.setValue(). See testSelect.ts for
// the shared openListbox()/optionByLabel() helpers.

afterEach(() => {
  document.body.innerHTML = ''
})

const BASE_PROPS = {
  layout: 'cards' as const,
  spawner: 'all',
  project: 'all',
  sortBy: 'latest' as AgentSort,
  groupBy: 'none' as AgentGroup,
  searchQuery: '',
  projectOptions: [{ value: 'all', label: 'All projects' }],
  spawnerOptions: [
    { value: 'all', label: 'All spawners' },
    { value: 'claude', label: 'Claude Code' },
  ],
  totalCount: 40,
  shownCount: 40,
}

function mountToolbar(overrides: Partial<typeof BASE_PROPS> = {}) {
  return mount(DashboardToolbar, { props: { ...BASE_PROPS, ...overrides }, attachTo: document.body })
}

async function openFilterMenu(w: ReturnType<typeof mountToolbar>) {
  await w.get('[data-testid="filter-menu"] > button').trigger('click')
  return w
}

describe('dashboardToolbar', () => {
  it('emits update:searchQuery from the toolbar search field', async () => {
    const w = mountToolbar()
    await w.get('[data-testid="toolbar-search"]').setValue('shop')
    expect(w.emitted('update:searchQuery')![0]).toEqual(['shop'])
  })

  it('keeps the filter selects behind the filter menu until it is opened', async () => {
    const w = mountToolbar()
    expect(w.find('[data-testid="select-project"]').isVisible()).toBe(false)
    await openFilterMenu(w)
    expect(w.get('[data-testid="select-project"]').isVisible()).toBe(true)
  })

  it('emits update:project when the project select changes', async () => {
    const w = mountToolbar({
      projectOptions: [
        { value: 'all', label: 'All projects' },
        { value: 'my-project', label: 'my-project' },
      ],
    })
    await openFilterMenu(w)
    const panel = await openListbox(w.get('[data-testid="select-project"]'))
    optionByLabel(panel, 'my-project').dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await w.vm.$nextTick()
    expect(w.emitted('update:project')?.[0]).toEqual(['my-project'])
  })

  it('drops the spawner filter entirely when no spawner is configured', async () => {
    const w = mountToolbar({ spawnerOptions: [{ value: 'all', label: 'All spawners' }] })
    await openFilterMenu(w)
    expect(w.find('[data-testid="select-spawner"]').exists()).toBe(false)
  })

  it('counts only the menu filters in the badge, not the search', async () => {
    const w = mountToolbar({ project: 'my-project', searchQuery: 'shop' })
    const trigger = w.get('[data-testid="filter-menu"] > button')
    expect(trigger.text()).toContain('1')
    // aria-label overrides the child nodes, so the badge only reaches assistive
    // technology if the name spells it out.
    expect(trigger.attributes('aria-label')).toBe('Filter agents, 1 active')
  })

  it('emits update:sortBy when the sort select changes', async () => {
    const w = mountToolbar()
    const panel = await openListbox(w.get('[data-testid="select-sort"]'))
    optionByLabel(panel, 'Longest running').dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await w.vm.$nextTick()
    expect(w.emitted('update:sortBy')?.[0]).toEqual(['longest'])
  })

  it('emits update:groupBy when the group select changes', async () => {
    const w = mountToolbar()
    const panel = await openListbox(w.get('[data-testid="select-group"]'))
    optionByLabel(panel, 'Status').dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await w.vm.$nextTick()
    expect(w.emitted('update:groupBy')?.[0]).toEqual(['status'])
  })

  it('offers spawner grouping while no spawner is filtered', async () => {
    const w = mountToolbar()
    const panel = await openListbox(w.get('[data-testid="select-group"]'))
    expect(panel.querySelectorAll('[role="option"]')).toHaveLength(5)
    expect(optionByLabel(panel, 'Spawner')).toBeTruthy()
  })

  it('hides spawner grouping once a spawner is filtered', async () => {
    const w = mountToolbar({ spawner: 'claude' })
    const panel = await openListbox(w.get('[data-testid="select-group"]'))
    const labels = [...panel.querySelectorAll('[role="option"]')].map(o => o.textContent?.trim())
    expect(labels).toHaveLength(4)
    expect(labels).not.toContain('Spawner')
  })

  // The resolved grouping now comes from useViewState, so the control renders
  // exactly what it is handed instead of second-guessing it.
  it('renders the grouping it is given', () => {
    const w = mountToolbar({ spawner: 'claude', groupBy: 'none' })
    expect(w.get('[data-testid="select-group"]').text()).toContain('No grouping')
  })

  it('moves the density toggle into the view overflow menu', async () => {
    const w = mountToolbar()
    expect(w.find('[data-testid="layout-list"]').isVisible()).toBe(false)
    await w.get('button[aria-label="More view options"]').trigger('click')
    const compact = w.get('[data-testid="layout-list"]')
    expect(compact.isVisible()).toBe(true)
    await compact.trigger('click')
    expect(w.emitted('update:layout')![0]).toEqual(['list'])
    // Picking a density dismisses the menu, as a menu selection should.
    expect(compact.isVisible()).toBe(false)
  })

  it('renders no applied-filter row while nothing narrows the roster', () => {
    const w = mountToolbar()
    expect(w.find('[data-testid="active-filters"]').exists()).toBe(false)
    expect(w.get('[data-testid="agent-count"]').text()).toBe('40 agents')
  })

  it('lists the search term among the applied filters', () => {
    const w = mountToolbar({ searchQuery: 'shop', shownCount: 3 })
    expect(w.get('[data-testid="active-filters"]').text()).toContain('Search: "shop"')
    expect(w.get('[data-testid="agent-count"]').text()).toContain('3 / 40')
  })

  it('clears the search from its own chip', async () => {
    const w = mountToolbar({ searchQuery: 'shop' })
    await w.get('[data-testid="clear-search"]').trigger('click')
    expect(w.emitted('update:searchQuery')![0]).toEqual([''])
  })

  it('clears search and both filters with one "Clear all"', async () => {
    const w = mountToolbar({ searchQuery: 'shop', project: 'my-project', spawner: 'claude' })
    await w.get('[data-testid="clear-all-filters"]').trigger('click')
    expect(w.emitted('update:searchQuery')![0]).toEqual([''])
    expect(w.emitted('update:project')![0]).toEqual(['all'])
    expect(w.emitted('update:spawner')![0]).toEqual(['all'])
  })
  // Clearing a chip removes the focused button; without a hand-off focus falls
  // to <body> and the keyboard user loses their place in the toolbar.
  it('moves focus to the next chip when one is cleared', async () => {
    const w = mountToolbar({ searchQuery: 'shop', project: 'my-project' })
    const clearSearch = w.get('[data-testid="clear-search"]')
    ;(clearSearch.element as HTMLElement).focus()

    await clearSearch.trigger('click')
    await nextTick()

    expect(document.activeElement).toBe(w.get('[data-testid="clear-project"]').element)
  })

  it('falls back to the search field when the last chip is cleared', async () => {
    const w = mountToolbar({ searchQuery: 'shop' })
    await w.get('[data-testid="clear-search"]').trigger('click')
    await nextTick()

    expect(document.activeElement).toBe(w.get('[data-testid="toolbar-search"]').element)
  })

  it('focuses the search field after "Clear all"', async () => {
    const w = mountToolbar({ searchQuery: 'shop', project: 'my-project', spawner: 'claude' })
    await w.get('[data-testid="clear-all-filters"]').trigger('click')
    await nextTick()

    expect(document.activeElement).toBe(w.get('[data-testid="toolbar-search"]').element)
  })
})
