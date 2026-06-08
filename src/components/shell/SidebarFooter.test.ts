import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import SidebarFooter from './SidebarFooter.vue'

const base = {
  expanded: true,
  totalCostLabel: '$2.34',
  todayCostLabel: '$5.00',
  totalTokensLabel: '1.2M',
  quotaPct: 73,
  theme: 'dark' as const,
  canInstall: false,
}

describe('sidebarFooter', () => {
  it('shows cost + tokens when expanded', () => {
    const w = mount(SidebarFooter, { props: base })
    expect(w.text()).toContain('$2.34')
    expect(w.text()).toContain('1.2M')
  })

  it('renders a quota progressbar with aria-valuenow', () => {
    const w = mount(SidebarFooter, { props: base })
    expect(w.get('[role="progressbar"]').attributes('aria-valuenow')).toBe('73')
  })

  it('emits open-sessions / open-settings / toggle-theme', async () => {
    const w = mount(SidebarFooter, { props: base })
    await w.get('[data-testid="footer-sessions"]').trigger('click')
    await w.get('[data-testid="footer-settings"]').trigger('click')
    await w.get('[data-testid="footer-theme"]').trigger('click')
    expect(w.emitted('openSessions')).toHaveLength(1)
    expect(w.emitted('openSettings')).toHaveLength(1)
    expect(w.emitted('toggleTheme')).toHaveLength(1)
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
