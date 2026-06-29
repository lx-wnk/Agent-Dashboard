import { afterEach, expect, it, vi } from 'vitest'
import { usePermissionPresets } from './usePermissionPresets'

afterEach(() => {
  vi.restoreAllMocks()
})

it('load populates presets with projectCwd and entries array', async () => {
  const payload = [
    {
      projectCwd: '/home/user/my-project',
      entries: [
        { tool: 'Bash', pattern: null },
        { tool: 'Read', pattern: '/src/**' },
      ],
    },
  ]
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(
    new Response(JSON.stringify(payload), { status: 200 }),
  )
  const { presets, load } = usePermissionPresets()
  await load()
  expect(presets.value).toHaveLength(1)
  expect(presets.value[0].projectCwd).toBe('/home/user/my-project')
  expect(presets.value[0].entries).toHaveLength(2)
  expect(presets.value[0].entries[0].tool).toBe('Bash')
  expect(presets.value[0].entries[1].pattern).toBe('/src/**')
})

it('revoke sends DELETE to /api/settings/permission-presets with cwd in body', async () => {
  const fetchMock = vi.spyOn(globalThis, 'fetch')
    .mockResolvedValue(new Response(JSON.stringify([]), { status: 200 }))
  const { revoke } = usePermissionPresets()
  await revoke('/home/user/my-project')
  const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
  expect(url).toBe('/api/settings/permission-presets')
  expect(init.method).toBe('DELETE')
  expect(JSON.parse(init.body as string)).toEqual({ cwd: '/home/user/my-project' })
})

it('revoke throws when the server responds with an error', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(
    new Response('Bad Request', { status: 400 }),
  )
  const { revoke } = usePermissionPresets()
  await expect(revoke('/home/user/my-project')).rejects.toThrow()
})
