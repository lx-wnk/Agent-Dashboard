import { afterEach, describe, expect, it, vi } from 'vitest'
import { usePluginSettings } from '../usePluginSettings'

// onMounted is registered but never invoked in a plain vitest context (no
// component instance), so no initial fetchPlugins fires automatically.

describe('usePluginSettings.update', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('posts to /api/plugins/{id}/update with Origin header', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true }))
    const { update } = usePluginSettings()
    await update('my-plugin')
    expect(vi.mocked(globalThis.fetch)).toHaveBeenCalledWith(
      '/api/plugins/my-plugin/update',
      expect.objectContaining({
        method: 'POST',
        headers: expect.objectContaining({ Origin: expect.any(String) }),
      }),
    )
  })

  it('throws with HTTP status when response is not ok', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 409,
      json: async () => ({ error: 'illegal transition' }),
    }))
    const { update } = usePluginSettings()
    await expect(update('my-plugin')).rejects.toThrow('illegal transition')
  })
})

describe('pluginView type', () => {
  it('healthy field is present in the interface (type-only check)', () => {
    // If PluginView does not have healthy, TypeScript compilation fails here.
    const view: import('../usePluginSettings').PluginView = {
      id: 'p',
      name: 'P',
      version: '1.0',
      state: 'active',
      updateAvailable: false,
      healthy: true,
      capabilities: [],
      hasSettings: false,
    }
    expect(view.healthy).toBe(true)
  })
})
