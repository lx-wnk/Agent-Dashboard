import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import DashboardToolbar from './DashboardToolbar.vue'

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

  it('renders project select with provided options', () => {
    const props = {
      ...BASE_PROPS,
      projectOptions: [
        { value: 'all', label: 'All projects' },
        { value: 'my-project', label: 'my-project' },
      ],
    }
    const w = mount(DashboardToolbar, { props })
    const select = w.get('[data-testid="select-project"]')
    expect(select.findAll('option')).toHaveLength(2)
  })

  it('emits update:project when project select changes', async () => {
    const w = mount(DashboardToolbar, { props: BASE_PROPS })
    const select = w.get('[data-testid="select-project"] select, [data-testid="select-project"]')
    await select.setValue('my-project')
    expect(w.emitted('update:project')).toBeTruthy()
  })

  it('renders spawner select with provided options', () => {
    const w = mount(DashboardToolbar, { props: BASE_PROPS })
    const select = w.get('[data-testid="select-spawner"]')
    expect(select.findAll('option')).toHaveLength(2)
  })

  it('emits update:spawner when spawner select changes', async () => {
    const w = mount(DashboardToolbar, { props: BASE_PROPS })
    const select = w.get('[data-testid="select-spawner"]')
    await select.setValue('claude')
    expect(w.emitted('update:spawner')).toBeTruthy()
  })

  it('emits update:sortBy when sort select changes', async () => {
    const w = mount(DashboardToolbar, { props: BASE_PROPS })
    const select = w.get('[data-testid="select-sort"]')
    await select.setValue('longest')
    expect(w.emitted('update:sortBy')).toBeTruthy()
  })

  it('emits update:groupBy when group select changes', async () => {
    const w = mount(DashboardToolbar, { props: BASE_PROPS })
    const select = w.get('[data-testid="select-group"]')
    await select.setValue('status')
    expect(w.emitted('update:groupBy')).toBeTruthy()
  })
})
