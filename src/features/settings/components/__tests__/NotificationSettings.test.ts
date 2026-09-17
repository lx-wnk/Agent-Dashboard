import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import NotificationSettings from '@/features/settings/components/NotificationSettings.vue'
import * as usePushSubscriptionMod from '@/features/settings/composables/usePushSubscription'

vi.mock('@/features/settings/composables/usePushSubscription', async () => {
  const actual = await vi.importActual<typeof import('@/features/settings/composables/usePushSubscription')>('@/features/settings/composables/usePushSubscription')
  return {
    ...actual,
    usePushSubscription: vi.fn(),
  }
})

const defaultPrefs = [{ eventType: 'on_hold', channels: ['webhook'], enabled: true }]
const defaultConfig = { webhook_url: 'https://example.com' }

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string) => {
    if (url === '/api/notifications/preferences')
      return Promise.resolve({ ok: true, json: () => Promise.resolve(defaultPrefs), status: 200 })
    if (url === '/api/notifications/config')
      return Promise.resolve({ ok: true, json: () => Promise.resolve(defaultConfig), status: 200 })
    return Promise.resolve({ ok: true, json: () => Promise.resolve({}), status: 200 })
  }))
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('notificationSettings — push block', () => {
  it('checks the existing subscription when it mounts', async () => {
    const refresh = vi.fn().mockResolvedValue(undefined)
    vi.mocked(usePushSubscriptionMod.usePushSubscription).mockReturnValue({
      supported: ref(true) as any,
      state: ref('unknown') as any,
      error: ref(null),
      refresh,
      enable: vi.fn(),
    })

    const wrapper = mount(NotificationSettings)
    await flushPromises()

    expect(refresh).toHaveBeenCalledOnce()

    wrapper.unmount()
  })

  it('shows the enable button when not subscribed and calls enable on click', async () => {
    const enable = vi.fn().mockResolvedValue(undefined)
    vi.mocked(usePushSubscriptionMod.usePushSubscription).mockReturnValue({
      supported: ref(true) as any,
      state: ref('not-subscribed') as any,
      error: ref(null),
      refresh: vi.fn().mockResolvedValue(undefined),
      enable,
    })

    const wrapper = mount(NotificationSettings)
    await flushPromises()

    const button = wrapper.findAll('button').find(b => b.text() === 'Enable push on this device')
    expect(button).toBeTruthy()
    expect(wrapper.text()).toContain('Push is off for this device')

    await button!.trigger('click')
    expect(enable).toHaveBeenCalledOnce()

    wrapper.unmount()
  })

  it('shows the subscribed status and no button when already subscribed', async () => {
    vi.mocked(usePushSubscriptionMod.usePushSubscription).mockReturnValue({
      supported: ref(true) as any,
      state: ref('subscribed') as any,
      error: ref(null),
      refresh: vi.fn().mockResolvedValue(undefined),
      enable: vi.fn(),
    })

    const wrapper = mount(NotificationSettings)
    await flushPromises()

    expect(wrapper.text()).toContain('Push is on for this device')
    expect(wrapper.findAll('button').find(b => b.text() === 'Enable push on this device')).toBeUndefined()

    wrapper.unmount()
  })

  it('shows the unsupported message and no button when push is unsupported', async () => {
    vi.mocked(usePushSubscriptionMod.usePushSubscription).mockReturnValue({
      supported: ref(false) as any,
      state: ref('unknown') as any,
      error: ref(null),
      refresh: vi.fn().mockResolvedValue(undefined),
      enable: vi.fn(),
    })

    const wrapper = mount(NotificationSettings)
    await flushPromises()

    expect(wrapper.text()).toContain('This browser does not support push notifications')
    expect(wrapper.findAll('button').find(b => b.text() === 'Enable push on this device')).toBeUndefined()

    wrapper.unmount()
  })
})
