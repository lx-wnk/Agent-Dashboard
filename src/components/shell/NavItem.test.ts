import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import NavItem from './NavItem.vue'

describe('navItem', () => {
  it('renders label when expanded', () => {
    const w = mount(NavItem, { props: { icon: '▦', label: 'Dashboard', active: false, expanded: true } })
    expect(w.text()).toContain('Dashboard')
  })

  it('hides label text when collapsed (icon-only)', () => {
    const w = mount(NavItem, { props: { icon: '▦', label: 'Dashboard', active: false, expanded: false } })
    expect(w.find('.sr-only').exists()).toBe(true)
  })

  it('sets aria-current=page when active', () => {
    const w = mount(NavItem, { props: { icon: '▦', label: 'Dashboard', active: true, expanded: true } })
    expect(w.get('button').attributes('aria-current')).toBe('page')
  })

  it('emits select on click', async () => {
    const w = mount(NavItem, { props: { icon: '▦', label: 'Dashboard', active: false, expanded: true } })
    await w.get('button').trigger('click')
    expect(w.emitted('select')).toHaveLength(1)
  })

  it('renders badge slot content', () => {
    const w = mount(NavItem, {
      props: { icon: '▦', label: 'Dashboard', active: false, expanded: true },
      slots: { badge: '12' },
    })
    expect(w.text()).toContain('12')
  })

  it('renders a focus-visible tooltip with the label when collapsed', () => {
    const w = mount(NavItem, { props: { icon: '▦', label: 'Dashboard', active: false, expanded: false } })
    const tooltip = w.find('.nav-tooltip')
    expect(tooltip.exists()).toBe(true)
    expect(tooltip.attributes('aria-hidden')).toBe('true')
    expect(tooltip.text()).toBe('Dashboard')
  })

  it('omits the tooltip element when expanded (label already visible)', () => {
    const w = mount(NavItem, { props: { icon: '▦', label: 'Dashboard', active: false, expanded: true } })
    expect(w.find('.nav-tooltip').exists()).toBe(false)
  })
})
