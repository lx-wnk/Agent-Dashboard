import { describe, expect, it, vi } from 'vitest'
import { loadSlotAddons } from './usePluginSlots'

const addonFor = (slot: string) => ({ default: { slot, mount: () => () => {} } })

describe('loadSlotAddons', () => {
  it('returns only addons whose module targets the requested slot', async () => {
    const fetchPlugins = vi.fn().mockResolvedValue([
      { id: 'voice-whisper', capabilities: ['route_extension'] },
      { id: 'other', capabilities: ['route_extension'] },
      { id: 'auth', capabilities: ['auth_provider'] },
    ])
    const importAddon = vi.fn(async (url: string) => {
      if (url === '/api/settings/plugins/voice-whisper/addon.js')
        return addonFor('refinement-input-addon')
      if (url === '/api/settings/plugins/other/addon.js')
        return addonFor('some-other-slot')
      throw new Error('404')
    })

    const addons = await loadSlotAddons('refinement-input-addon', { fetchPlugins, importAddon })

    expect(addons).toHaveLength(1)
    expect(addons[0].slot).toBe('refinement-input-addon')
    expect(importAddon).not.toHaveBeenCalledWith('/api/settings/plugins/auth/addon.js')
  })

  it('skips plugins whose addon.js import fails', async () => {
    const fetchPlugins = vi.fn().mockResolvedValue([
      { id: 'no-addon', capabilities: ['route_extension'] },
    ])
    const importAddon = vi.fn().mockRejectedValue(new Error('404'))

    const addons = await loadSlotAddons('refinement-input-addon', { fetchPlugins, importAddon })

    expect(addons).toEqual([])
  })

  it('returns [] when the plugin-list fetch fails (slot degrades gracefully)', async () => {
    const fetchPlugins = vi.fn().mockRejectedValue(new Error('HTTP 500'))
    const importAddon = vi.fn()
    const addons = await loadSlotAddons('refinement-input-addon', { fetchPlugins, importAddon })
    expect(addons).toEqual([])
    expect(importAddon).not.toHaveBeenCalled()
  })
})
