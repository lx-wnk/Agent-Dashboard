import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'

let usePushSubscription: typeof import('@/features/settings/composables/usePushSubscription').usePushSubscription

function withSetup<T>(composable: () => T) {
  let result!: T
  const Wrapper = defineComponent({
    setup() {
      result = composable()
      return {}
    },
    template: '<div />',
  })
  const wrapper = mount(Wrapper)
  return { result, wrapper }
}

const getSubscription = vi.fn()
const subscribe = vi.fn()
const requestPermission = vi.fn()

function stubPushSupport(): void {
  Object.defineProperty(navigator, 'serviceWorker', {
    configurable: true,
    value: { ready: Promise.resolve({ pushManager: { getSubscription, subscribe } }) },
  })
  vi.stubGlobal('PushManager', class {})
  vi.stubGlobal('Notification', { requestPermission })
}

beforeEach(async () => {
  vi.resetModules()
  vi.clearAllMocks()
  getSubscription.mockResolvedValue(null)
  requestPermission.mockResolvedValue('granted')
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve({}), status: 200 }))
  const mod = await import('@/features/settings/composables/usePushSubscription')
  usePushSubscription = mod.usePushSubscription
})

afterEach(() => {
  Reflect.deleteProperty(navigator, 'serviceWorker')
  vi.unstubAllGlobals()
})

describe('usePushSubscription', () => {
  it('supported is false when serviceWorker/PushManager are missing', () => {
    Reflect.deleteProperty(navigator, 'serviceWorker')
    const { result } = withSetup(() => usePushSubscription())
    expect(result.supported.value).toBe(false)
  })

  it('refresh sets subscribed when a subscription exists', async () => {
    stubPushSupport()
    getSubscription.mockResolvedValue({ endpoint: 'https://push.example/1' })
    const { result } = withSetup(() => usePushSubscription())

    await result.refresh()

    expect(result.state.value).toBe('subscribed')
  })

  it('refresh sets not-subscribed when there is no subscription', async () => {
    stubPushSupport()
    getSubscription.mockResolvedValue(null)
    const { result } = withSetup(() => usePushSubscription())

    await result.refresh()

    expect(result.state.value).toBe('not-subscribed')
  })

  it('enable sets denied and does not fetch when permission is denied', async () => {
    stubPushSupport()
    requestPermission.mockResolvedValue('denied')
    const { result } = withSetup(() => usePushSubscription())

    await result.enable()

    expect(result.state.value).toBe('denied')
    expect(globalThis.fetch).not.toHaveBeenCalled()
  })

  it('enable fetches the VAPID key, subscribes and posts the subscription', async () => {
    stubPushSupport()
    requestPermission.mockResolvedValue('granted')
    subscribe.mockResolvedValue({
      toJSON: () => ({ endpoint: 'https://push.example/1', keys: { p256dh: 'p256', auth: 'auth' } }),
    })
    const calls: string[] = []
    vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      calls.push(`${init?.method ?? 'GET'} ${url}`)
      if (url === '/api/settings/webpush/vapid' && (!init || init.method === undefined))
        return Promise.resolve({ ok: false, status: 404, json: () => Promise.resolve({}) })
      if (url === '/api/settings/webpush/vapid')
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ publicKey: 'QUJD' }) })
      if (url === '/api/settings/webpush/subscribe')
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ ok: true }) })
      return Promise.resolve({ ok: true, json: () => Promise.resolve({}) })
    }))
    const { result } = withSetup(() => usePushSubscription())

    await result.enable()

    expect(subscribe).toHaveBeenCalledWith({
      userVisibleOnly: true,
      applicationServerKey: expect.any(Uint8Array),
    })
    expect(calls).toContain('POST /api/settings/webpush/subscribe')
    const subscribeBody = JSON.parse(
      vi.mocked(globalThis.fetch).mock.calls.find(([url]) => url === '/api/settings/webpush/subscribe')![1]!.body as string,
    )
    expect(subscribeBody).toEqual({ endpoint: 'https://push.example/1', keys: { p256dh: 'p256', auth: 'auth' } })
    expect(result.state.value).toBe('subscribed')
  })

  it('enable sets error with the server message when the subscribe POST fails', async () => {
    stubPushSupport()
    requestPermission.mockResolvedValue('granted')
    subscribe.mockResolvedValue({
      toJSON: () => ({ endpoint: 'https://push.example/1', keys: { p256dh: 'p256', auth: 'auth' } }),
    })
    vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string) => {
      if (url === '/api/settings/webpush/vapid')
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ publicKey: 'QUJD' }) })
      if (url === '/api/settings/webpush/subscribe') {
        return Promise.resolve({
          ok: false,
          status: 400,
          json: () => Promise.resolve({ error: 'endpoint is required' }),
        })
      }
      return Promise.resolve({ ok: true, json: () => Promise.resolve({}) })
    }))
    const { result } = withSetup(() => usePushSubscription())

    await result.enable()

    expect(result.state.value).toBe('error')
    expect(result.error.value).toBe('endpoint is required')
  })
})
