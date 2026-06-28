import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { useServerReconnect } from '../composables/useServerReconnect'
import ServerReconnectOverlay from './ServerReconnectOverlay.vue'

describe('serverReconnectOverlay', () => {
  it('is hidden when not reconnecting and shown when reconnecting', async () => {
    const { isReconnecting } = useServerReconnect()
    isReconnecting.value = false
    const wrapper = mount(ServerReconnectOverlay)
    expect(wrapper.text()).not.toContain('restarting')

    isReconnecting.value = true
    await wrapper.vm.$nextTick()
    expect(wrapper.text().toLowerCase()).toContain('restarting')
    isReconnecting.value = false // reset shared singleton
  })
})
