import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import SidebarFooter from './SidebarFooter.vue'

const base = {
  expanded: true,
  theme: 'dark' as const,
  canInstall: false,
}

describe('sidebarFooter', () => {
  it('emits open-sessions / toggle-theme', async () => {
    const w = mount(SidebarFooter, { props: base })
    await w.get('[data-testid="footer-sessions"]').trigger('click')
    await w.get('[data-testid="footer-theme"]').trigger('click')
    expect(w.emitted('openSessions')).toHaveLength(1)
    expect(w.emitted('toggleTheme')).toHaveLength(1)
  })

  it('has no quota bar or settings button (moved to status bar / topbar)', () => {
    const w = mount(SidebarFooter, { props: base })
    expect(w.find('[role="progressbar"]').exists()).toBe(false)
    expect(w.find('[data-testid="footer-settings"]').exists()).toBe(false)
  })

  it('hides install button unless canInstall', () => {
    const w = mount(SidebarFooter, { props: base })
    expect(w.find('[data-testid="footer-install"]').exists()).toBe(false)
  })

  it('shows install button and emits install when canInstall', async () => {
    const w = mount(SidebarFooter, { props: { ...base, canInstall: true } })
    const btn = w.get('[data-testid="footer-install"]')
    expect(btn.text()).toContain('Install PWA')
    await btn.trigger('click')
    expect(w.emitted('install')).toHaveLength(1)
  })
})
