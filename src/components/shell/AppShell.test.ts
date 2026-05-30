import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import AppShell from './AppShell.vue'

describe('appShell', () => {
  it('renders all four slots', () => {
    const w = mount(AppShell, {
      slots: {
        sidebar: '<div>SIDEBAR</div>',
        topbar: '<div>TOPBAR</div>',
        default: '<div>CONTENT</div>',
        statusbar: '<div>STATUS</div>',
      },
    })
    const t = w.text()
    expect(t).toContain('SIDEBAR')
    expect(t).toContain('TOPBAR')
    expect(t).toContain('CONTENT')
    expect(t).toContain('STATUS')
  })

  it('has a skip-to-content link targeting #main-content', () => {
    const w = mount(AppShell)
    const link = w.find('a[href="#main-content"]')
    expect(link.exists()).toBe(true)
  })

  it('main region is focusable (tabindex -1) with id main-content', () => {
    const w = mount(AppShell)
    const main = w.get('#main-content')
    expect(main.attributes('tabindex')).toBe('-1')
  })
})
