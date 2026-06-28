import { describe, expect, it, vi } from 'vitest'
import { usePluginSettings } from './usePluginSettings'

describe('usePluginSettings', () => {
  it('toggle returns "restart" for auth-provider plugins and updates local state', async () => {
    const plugins = [{ id: 'github-oauth', capabilities: ['auth_provider'], enabled: false, healthy: true, authProvider: true }]
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => plugins }) // initial GET
      .mockResolvedValueOnce({ ok: true, json: async () => ({ id: 'github-oauth', enabled: true, applied: 'restart' }) })

    const s = usePluginSettings()
    await s.refetch()
    const applied = await s.toggle('github-oauth', true)
    expect(applied).toBe('restart')
    expect(s.plugins.value.find(p => p.id === 'github-oauth')?.enabled).toBe(true)
  })

  it('toggle returns "restart" for non-auth plugins and updates local state', async () => {
    const plugins = [{ id: 'metrics', capabilities: ['route_extension'], enabled: true, healthy: true, authProvider: false }]
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => plugins }) // initial GET
      .mockResolvedValueOnce({ ok: true, json: async () => ({ id: 'metrics', enabled: false, applied: 'restart' }) })

    const s = usePluginSettings()
    await s.refetch()
    const applied = await s.toggle('metrics', false)
    expect(applied).toBe('restart')
    expect(s.plugins.value.find(p => p.id === 'metrics')?.enabled).toBe(false)
  })
})
