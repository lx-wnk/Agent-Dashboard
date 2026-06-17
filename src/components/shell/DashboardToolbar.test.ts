import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import DashboardToolbar from './DashboardToolbar.vue'

const BASE_PROPS = {
  layout: 'cards' as const,
  hideNonClaude: false,
  project: 'all',
  sortBy: 'latest' as const,
  groupBy: 'none' as const,
  projectOptions: [{ value: 'all', label: 'All projects' }],
  count: 3,
  attentionCount: 0,
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

  it('emits update:hideNonClaude when toggling the filter', async () => {
    const w = mount(DashboardToolbar, { props: BASE_PROPS })
    await w.get('[data-testid="claude-only"]').trigger('click')
    expect(w.emitted('update:hideNonClaude')![0]).toEqual([true])
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

  it('shows agent count', () => {
    const w = mount(DashboardToolbar, { props: { ...BASE_PROPS, count: 5 } })
    expect(w.text()).toContain('5 agents')
  })

  it('shows attention count when non-zero', () => {
    const w = mount(DashboardToolbar, { props: { ...BASE_PROPS, count: 3, attentionCount: 2 } })
    expect(w.text()).toContain('2 need you')
  })

  it('omits attention suffix when zero', () => {
    const w = mount(DashboardToolbar, { props: BASE_PROPS })
    expect(w.text()).not.toContain('need you')
  })
})
