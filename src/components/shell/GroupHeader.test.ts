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
})
