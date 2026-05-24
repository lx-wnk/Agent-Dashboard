import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ProviderBadge from './ProviderBadge.vue'

describe('providerBadge', () => {
  it('renders nothing for claude (default provider)', () => {
    const wrapper = mount(ProviderBadge, { props: { provider: 'claude' } })
    expect(wrapper.find('span').exists()).toBe(false)
  })

  it('renders nothing when provider is undefined', () => {
    const wrapper = mount(ProviderBadge)
    expect(wrapper.find('span').exists()).toBe(false)
  })

  it('renders nothing when provider is null', () => {
    const wrapper = mount(ProviderBadge, { props: { provider: null } })
    expect(wrapper.find('span').exists()).toBe(false)
  })

  it('renders a badge for codex with its title', () => {
    const wrapper = mount(ProviderBadge, { props: { provider: 'codex' } })
    const badge = wrapper.find('span')
    expect(badge.exists()).toBe(true)
    expect(badge.attributes('title')).toBe('Provider: Codex')
    expect(badge.text()).toBe('O')
  })

  it('renders a badge for gemini with its title', () => {
    const wrapper = mount(ProviderBadge, { props: { provider: 'gemini' } })
    const badge = wrapper.find('span')
    expect(badge.exists()).toBe(true)
    expect(badge.attributes('title')).toBe('Provider: Gemini')
    expect(badge.text()).toBe('G')
  })
})
