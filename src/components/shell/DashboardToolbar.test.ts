import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import DashboardToolbar from './DashboardToolbar.vue'

describe('dashboardToolbar', () => {
  it('marks the active layout button aria-pressed', () => {
    const w = mount(DashboardToolbar, { props: { layout: 'cards', hideNonClaude: false } })
    const cards = w.get('[data-testid="layout-cards"]')
    expect(cards.attributes('aria-pressed')).toBe('true')
  })

  it('emits update:layout when clicking List', async () => {
    const w = mount(DashboardToolbar, { props: { layout: 'cards', hideNonClaude: false } })
    await w.get('[data-testid="layout-list"]').trigger('click')
    expect(w.emitted('update:layout')![0]).toEqual(['list'])
  })

  it('emits update:hideNonClaude when toggling the filter', async () => {
    const w = mount(DashboardToolbar, { props: { layout: 'cards', hideNonClaude: false } })
    await w.get('[data-testid="claude-only"]').trigger('click')
    expect(w.emitted('update:hideNonClaude')![0]).toEqual([true])
  })
})
