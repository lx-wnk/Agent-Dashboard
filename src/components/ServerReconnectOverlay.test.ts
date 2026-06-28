import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { _resetForTesting, useServerReconnect } from '../composables/useServerReconnect'
import ServerReconnectOverlay from './ServerReconnectOverlay.vue'

describe('serverReconnectOverlay', () => {
  afterEach(() => {
    _resetForTesting()
    vi.unstubAllGlobals()
  })

  it('is hidden when not reconnecting and shown when reconnecting', async () => {
    const { isReconnecting } = useServerReconnect()
    isReconnecting.value = false
    const wrapper = mount(ServerReconnectOverlay)
    expect(wrapper.text()).not.toContain('restarting')

    isReconnecting.value = true
    await wrapper.vm.$nextTick()
    expect(wrapper.text().toLowerCase()).toContain('restarting')
  })

  it('shows stalled message and manual reload button when stalled', async () => {
    const reload = vi.fn()
    vi.stubGlobal('location', { reload } as any)
    const { isReconnecting, stalled } = useServerReconnect()
    isReconnecting.value = true
    stalled.value = true
    const wrapper = mount(ServerReconnectOverlay)
    await wrapper.vm.$nextTick()
    expect(wrapper.text().toLowerCase()).toContain('reload')
    const btn = wrapper.find('button')
    expect(btn.exists()).toBe(true)
    await btn.trigger('click')
    expect(reload).toHaveBeenCalledOnce()
  })
})
