import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import SpotlightSearch from './SpotlightSearch.vue'

vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
  ok: true,
  json: async () => ({ tasks: [], agents: [] }),
}))

describe('spotlightSearch', () => {
  it('is hidden by default', () => {
    const wrapper = mount(SpotlightSearch, { attachTo: document.body })
    // Input is inside AppModal which teleports to body — check document directly
    expect(document.querySelector('input[placeholder]')).toBeNull()
    wrapper.unmount()
  })

  it('opens on Cmd+K', async () => {
    const wrapper = mount(SpotlightSearch, { attachTo: document.body })
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', metaKey: true }))
    await wrapper.vm.$nextTick()
    // After opening, AppModal renders its slot into body via Teleport
    expect(document.querySelector('input[placeholder]')).not.toBeNull()
    wrapper.unmount()
  })
})
