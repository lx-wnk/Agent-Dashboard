import type { Agent } from '../../types'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import GroupHeader from './GroupHeader.vue'

function makeAgent(cost: number): Partial<Agent> {
  return { costEstimate: cost } as Partial<Agent>
}

describe('groupHeader', () => {
  it('renders the label', () => {
    const w = mount(GroupHeader, { props: { label: 'Active', agents: [] } })
    expect(w.text()).toContain('Active')
  })

  it('renders agent count singular', () => {
    const w = mount(GroupHeader, {
      props: { label: 'Active', agents: [makeAgent(0)] as Agent[] },
    })
    expect(w.text()).toContain('1 agent')
    expect(w.text()).not.toContain('agents')
  })

  it('renders agent count plural', () => {
    const w = mount(GroupHeader, {
      props: { label: 'Active', agents: [makeAgent(0), makeAgent(0)] as Agent[] },
    })
    expect(w.text()).toContain('2 agents')
  })

  it('renders summed cost', () => {
    const w = mount(GroupHeader, {
      props: { label: 'Active', agents: [makeAgent(1.5), makeAgent(0.5)] as Agent[] },
    })
    expect(w.text()).toContain('$2.00 today')
  })

  it('renders — when total cost is zero', () => {
    const w = mount(GroupHeader, {
      props: { label: 'Active', agents: [makeAgent(0)] as Agent[] },
    })
    expect(w.text()).toContain('— today')
  })

  it('renders an expanded chevron when not collapsed', () => {
    const w = mount(GroupHeader, { props: { label: 'Active', agents: [] } })
    const btn = w.find('[data-testid="group-header-toggle"]')
    expect(btn.text()).toContain('▼')
    expect(btn.attributes('aria-expanded')).toBe('true')
  })

  it('renders a collapsed chevron when collapsed', () => {
    const w = mount(GroupHeader, { props: { label: 'Active', agents: [], collapsed: true } })
    const btn = w.find('[data-testid="group-header-toggle"]')
    expect(btn.text()).toContain('▶')
    expect(btn.attributes('aria-expanded')).toBe('false')
  })

  it('emits toggle when the header is clicked', async () => {
    const w = mount(GroupHeader, { props: { label: 'Active', agents: [] } })
    await w.find('[data-testid="group-header-toggle"]').trigger('click')
    expect(w.emitted('toggle')).toBeTruthy()
  })
})
