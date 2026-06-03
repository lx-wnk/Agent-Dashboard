import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import LivePulse from './LivePulse.vue'

describe('livePulse', () => {
  it('shows Live when connected', () => {
    const w = mount(LivePulse, { props: { live: true } })
    expect(w.text()).toContain('Live')
  })

  it('shows Reconnecting when not live', () => {
    const w = mount(LivePulse, { props: { live: false } })
    expect(w.text()).toContain('Reconnecting')
  })

  it('pulse dot disables animation under reduced motion', () => {
    const w = mount(LivePulse, { props: { live: true } })
    expect(w.get('[data-dot]').classes()).toContain('motion-reduce:animate-none')
  })

  it('exposes a status role for screen readers', () => {
    const w = mount(LivePulse, { props: { live: true } })
    expect(w.find('[role="status"]').exists()).toBe(true)
  })
})
