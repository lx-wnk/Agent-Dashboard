import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'

let useNotificationConfig: typeof import('@/features/settings/composables/useNotificationConfig').useNotificationConfig

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

const defaultPrefs = [
  { eventType: 'on_hold', channels: ['webhook'], enabled: true },
]
const defaultConfig = { webhook_url: 'https://example.com' }

beforeEach(async () => {
  vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string) => {
    if (url === '/api/notifications/preferences') {
      return Promise.resolve({ ok: true, json: () => Promise.resolve(defaultPrefs), status: 200 })
    }
    if (url === '/api/notifications/config') {
      return Promise.resolve({ ok: true, json: () => Promise.resolve(defaultConfig), status: 200 })
    }
    return Promise.resolve({ ok: true, json: () => Promise.resolve({}), status: 200 })
  }))
  vi.resetModules()
  const mod = await import('@/features/settings/composables/useNotificationConfig')
  useNotificationConfig = mod.useNotificationConfig
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useNotificationConfig', () => {
  it('fetches prefs and config on mount', async () => {
    const { result } = withSetup(() => useNotificationConfig())
    await vi.waitUntil(() => !result.loading.value)

    expect(result.prefs.value.on_hold).toMatchObject({ enabled: true, channels: ['webhook'] })
    expect(result.config.value).toMatchObject({ webhook_url: 'https://example.com' })
  })

  it('refetch re-fetches data', async () => {
    const { result } = withSetup(() => useNotificationConfig())
    await vi.waitUntil(() => !result.loading.value)

    vi.mocked(globalThis.fetch).mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve([{ eventType: 'completed', channels: [], enabled: false }]),
      status: 200,
    } as Response).mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve({}),
      status: 200,
    } as Response)

    await result.refetch()
    expect(result.prefs.value.completed).toMatchObject({ enabled: false })
  })

  it('sets error when fetch fails', async () => {
    vi.mocked(globalThis.fetch).mockResolvedValue({
      ok: false,
      status: 500,
      json: () => Promise.resolve({}),
    } as Response)
    vi.resetModules()
    const mod = await import('@/features/settings/composables/useNotificationConfig')
    useNotificationConfig = mod.useNotificationConfig

    const { result } = withSetup(() => useNotificationConfig())
    await vi.waitUntil(() => !result.loading.value)

    expect(result.error.value).toBeTruthy()
  })
})
