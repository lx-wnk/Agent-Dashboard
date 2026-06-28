import { beforeEach, describe, expect, it, vi } from 'vitest'
import { loadSlotAddons, resetSlotCaches } from './usePluginSlots'

const addonFor = (slot: string) => ({ default: { slot, mount: () => () => {} } })
const bareAddon = () => ({ default: { mount: () => () => {} } })

beforeEach(() => {
  resetSlotCaches()
})

describe('loadSlotAddons — legacy addon.js path', () => {
  it('returns only addons whose module targets the requested slot', async () => {
    const fetchPlugins = vi.fn().mockResolvedValue([
      { id: 'voice-whisper', capabilities: ['route_extension'] },
      { id: 'other', capabilities: ['route_extension'] },
      { id: 'auth', capabilities: ['auth_provider'] },
    ])
    const fetchManifest = vi.fn().mockResolvedValue(null)
    const importAddon = vi.fn(async (url: string) => {
      if (url === '/api/plugins/voice-whisper/proxy/addon.js')
        return addonFor('refinement-input-addon')
      if (url === '/api/plugins/other/proxy/addon.js')
        return addonFor('some-other-slot')
      throw new Error('404')
    })

    const addons = await loadSlotAddons('refinement-input-addon', { fetchPlugins, fetchManifest, importAddon })

    expect(addons).toHaveLength(1)
    expect(addons[0].slot).toBe('refinement-input-addon')
    expect(importAddon).not.toHaveBeenCalledWith('/api/plugins/auth/proxy/addon.js')
  })

  it('skips plugins whose addon.js import fails', async () => {
    const fetchPlugins = vi.fn().mockResolvedValue([
      { id: 'no-addon', capabilities: ['route_extension'] },
    ])
    const fetchManifest = vi.fn().mockResolvedValue(null)
    const importAddon = vi.fn().mockRejectedValue(new Error('404'))

    const addons = await loadSlotAddons('refinement-input-addon', { fetchPlugins, fetchManifest, importAddon })

    expect(addons).toEqual([])
  })

  it('returns [] when the plugin-list fetch fails (slot degrades gracefully)', async () => {
    const fetchPlugins = vi.fn().mockRejectedValue(new Error('HTTP 500'))
    const importAddon = vi.fn()
    const addons = await loadSlotAddons('refinement-input-addon', { fetchPlugins, importAddon })
    expect(addons).toEqual([])
    expect(importAddon).not.toHaveBeenCalled()
  })

  it('falls back to addon.js when a ui_extension plugin ships no manifest (404)', async () => {
    const fetchPlugins = vi.fn().mockResolvedValue([
      { id: 'voice', capabilities: ['route_extension'] },
    ])
    const fetchManifest = vi.fn().mockResolvedValue(null) // 404 → null
    const importAddon = vi.fn(async () => addonFor('refinement-input-addon'))

    const addons = await loadSlotAddons('refinement-input-addon', { fetchPlugins, fetchManifest, importAddon })

    expect(importAddon).toHaveBeenCalledWith('/api/plugins/voice/proxy/addon.js')
    expect(addons).toHaveLength(1)
  })
})

describe('loadSlotAddons — UI manifest path', () => {
  it('imports only the module listed for the requested slot', async () => {
    const fetchPlugins = vi.fn().mockResolvedValue([
      { id: 'rich', capabilities: ['ui_extension'] },
    ])
    const fetchManifest = vi.fn(async () => ({
      slots: [
        { slot: 'task-modal-footer', module: 'footer.js' },
        { slot: 'kanban-card-badge', module: 'badge.js' },
      ],
    }))
    const importAddon = vi.fn(async () => bareAddon())

    const addons = await loadSlotAddons('task-modal-footer', { fetchPlugins, fetchManifest, importAddon })

    expect(importAddon).toHaveBeenCalledTimes(1)
    expect(importAddon).toHaveBeenCalledWith('/api/plugins/rich/proxy/footer.js')
    expect(importAddon).not.toHaveBeenCalledWith('/api/plugins/rich/proxy/badge.js')
    expect(addons).toHaveLength(1)
    expect(addons[0].slot).toBe('task-modal-footer')
  })

  it('skips manifest modules that escape the plugin namespace', async () => {
    const fetchPlugins = vi.fn().mockResolvedValue([
      { id: 'rich', capabilities: ['ui_extension'] },
    ])
    const fetchManifest = vi.fn(async () => ({
      slots: [
        { slot: 'task-modal-footer', module: '../other/evil.js' },
        { slot: 'task-modal-footer', module: '/etc/passwd' },
        { slot: 'task-modal-footer', module: 'https://evil.example/x.js' },
      ],
    }))
    const importAddon = vi.fn(async () => bareAddon())

    const addons = await loadSlotAddons('task-modal-footer', { fetchPlugins, fetchManifest, importAddon })

    expect(importAddon).not.toHaveBeenCalled()
    expect(addons).toEqual([])
  })

  it('memoizes the plugin list, manifest, and module across repeat loads', async () => {
    const fetchPlugins = vi.fn().mockResolvedValue([
      { id: 'rich', capabilities: ['ui_extension'] },
    ])
    const fetchManifest = vi.fn(async () => ({
      slots: [{ slot: 'task-modal-footer', module: 'footer.js' }],
    }))
    const importAddon = vi.fn(async () => bareAddon())

    await loadSlotAddons('task-modal-footer', { fetchPlugins, fetchManifest, importAddon })
    await loadSlotAddons('task-modal-footer', { fetchPlugins, fetchManifest, importAddon })

    expect(fetchPlugins).toHaveBeenCalledTimes(1)
    expect(fetchManifest).toHaveBeenCalledTimes(1)
    expect(importAddon).toHaveBeenCalledTimes(1)
  })

  it('considers both ui_extension and route_extension plugins as candidates', async () => {
    const fetchPlugins = vi.fn().mockResolvedValue([
      { id: 'manifested', capabilities: ['ui_extension'] },
      { id: 'legacy', capabilities: ['route_extension'] },
      { id: 'auth', capabilities: ['auth_provider'] },
    ])
    const fetchManifest = vi.fn(async (id: string) =>
      id === 'manifested' ? { slots: [{ slot: 'settings-panel', module: 'panel.js' }] } : null)
    const importAddon = vi.fn(async (url: string) => {
      if (url.endsWith('panel.js'))
        return bareAddon()
      return addonFor('settings-panel')
    })

    const addons = await loadSlotAddons('settings-panel', { fetchPlugins, fetchManifest, importAddon })

    expect(addons).toHaveLength(2)
    expect(importAddon).not.toHaveBeenCalledWith('/api/plugins/auth/proxy/addon.js')
  })

  it('propagates priority and mode from the manifest entry', async () => {
    resetSlotCaches()
    const addons = await loadSlotAddons('task-modal-footer', {
      fetchPlugins: async () => [{ id: 'p1', capabilities: ['ui_extension'] }],
      fetchManifest: async () => ({ slots: [{ slot: 'task-modal-footer', module: 'a.js', priority: 50, mode: 'extend' }] }),
      importAddon: async () => ({ default: { mount: () => () => {} } }),
    })
    expect(addons).toHaveLength(1)
    expect(addons[0].priority).toBe(50)
    expect(addons[0].mode).toBe('extend')
  })
})
