import type { PresetProjectSummary } from '../usePermissionPresets'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const mockPresets: PresetProjectSummary[] = [
  { projectCwd: '/home/user/project-a', entries: [{ tool: 'Bash', pattern: null }] },
  { projectCwd: '/home/user/project-b', entries: [{ tool: 'Read', pattern: '/src/**' }] },
]

beforeEach(() => {
  vi.resetModules()
  vi.stubGlobal('fetch', vi.fn())
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('usePermissionPresets', () => {
  describe('load', () => {
    it('populates presets from the API response', async () => {
      vi.mocked(globalThis.fetch).mockResolvedValue({
        ok: true,
        json: () => Promise.resolve(mockPresets),
      } as Response)

      const { usePermissionPresets } = await import('../usePermissionPresets')
      const { presets, load } = usePermissionPresets()

      await load()

      expect(presets.value).toEqual(mockPresets)
    })

    it('leaves presets unchanged on a non-ok response', async () => {
      vi.mocked(globalThis.fetch).mockResolvedValue({
        ok: false,
        json: () => Promise.resolve([]),
      } as Response)

      const { usePermissionPresets } = await import('../usePermissionPresets')
      const { presets, load } = usePermissionPresets()

      await load()

      expect(presets.value).toEqual([])
    })

    it('swallows fetch errors silently', async () => {
      vi.mocked(globalThis.fetch).mockRejectedValue(new Error('network down'))

      const { usePermissionPresets } = await import('../usePermissionPresets')
      const { presets, load } = usePermissionPresets()

      await expect(load()).resolves.toBeUndefined()
      expect(presets.value).toEqual([])
    })
  })

  describe('revoke', () => {
    it('sends DELETE with the cwd body and reloads presets', async () => {
      const fetchMock = vi.mocked(globalThis.fetch)
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) } as Response)
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve([]) } as Response)

      const { usePermissionPresets } = await import('../usePermissionPresets')
      const { revoke } = usePermissionPresets()

      await revoke('/home/user/project-a')

      const [url, init] = fetchMock.mock.calls[0]
      expect(url).toBe('/api/settings/permission-presets')
      expect(init!.method).toBe('DELETE')
      expect(JSON.parse(init!.body as string)).toEqual({ cwd: '/home/user/project-a' })
      // Second call is the reload GET
      expect(fetchMock).toHaveBeenCalledTimes(2)
      expect(fetchMock.mock.calls[1][0]).toBe('/api/settings/permission-presets')
    })

    it('throws when the DELETE response is not ok', async () => {
      vi.mocked(globalThis.fetch).mockResolvedValue({
        ok: false,
        json: () => Promise.resolve({}),
      } as Response)

      const { usePermissionPresets } = await import('../usePermissionPresets')
      const { revoke } = usePermissionPresets()

      await expect(revoke('/home/user/project-a')).rejects.toThrow('Failed to revoke preset')
    })
  })
})
