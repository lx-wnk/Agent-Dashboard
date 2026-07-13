import { describe, expect, it, vi } from 'vitest'
import { useSettings } from '@/features/settings/composables/useSettings'

describe('useSettings', () => {
  it('patches a setting and updates local state', async () => {
    const settings = [{ key: 'spawn.rateLimit', type: 'int', value: '5', default: '5', apply: 'restart', category: 'spawn' }]
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => settings }) // initial GET
      .mockResolvedValueOnce({ ok: true, json: async () => ({ key: 'spawn.rateLimit', value: '9', applied: 'restart' }) })

    const s = useSettings()
    await s.refetch()
    const applied = await s.update('spawn.rateLimit', '9')
    expect(applied).toBe('restart')
    expect(s.items.value.find(i => i.key === 'spawn.rateLimit')?.value).toBe('9')
  })
})
