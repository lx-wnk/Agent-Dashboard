import { afterEach, expect, it, vi } from 'vitest'
import { usePluginSettings } from '@/features/plugins/composables/usePluginSettings'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

it('setActive posts activate and updates state', async () => {
  const fetchMock = vi.fn()
    .mockResolvedValueOnce({ ok: true, json: async () => [{ id: 'p1', name: 'P1', version: '1', state: 'inactive', updateAvailable: false, capabilities: ['ui_extension'], hasSettings: true }] })
    .mockResolvedValueOnce({ ok: true, json: async () => ({ id: 'p1', state: 'active' }) })
  vi.stubGlobal('fetch', fetchMock)
  const s = usePluginSettings()
  await s.refetch()
  await s.setActive('p1', true)
  expect(fetchMock).toHaveBeenLastCalledWith('/api/plugins/p1/activate', expect.objectContaining({ method: 'POST' }))
  expect(s.plugins.value[0].state).toBe('active')
})

it('getSettings + putSettings hit the settings endpoints', async () => {
  const fetchMock = vi.fn()
    .mockResolvedValueOnce({ ok: true, json: async () => ({ schema: [{ key: 'apiKey', type: 'string', label: 'API Key', secret: true }], values: { apiKey: '********' } }) })
    .mockResolvedValueOnce({ ok: true, status: 204 })
  vi.stubGlobal('fetch', fetchMock)
  const s = usePluginSettings()
  const got = await s.getSettings('p1')
  expect(got.schema[0].key).toBe('apiKey')
  await s.putSettings('p1', { apiKey: 'new-secret' })
  expect(fetchMock).toHaveBeenLastCalledWith('/api/plugins/p1/settings', expect.objectContaining({ method: 'PUT' }))
})
