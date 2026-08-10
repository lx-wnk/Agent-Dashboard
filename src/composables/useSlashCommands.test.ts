import { describe, expect, it, vi } from 'vitest'
import { dispatchSlashCommand, fetchDynamicCommands, parseSlashCommand } from './useSlashCommands'

describe('parseSlashCommand', () => {
  it('returns null for non-slash input', () => {
    expect(parseSlashCommand('hello world')).toBeNull()
    expect(parseSlashCommand('')).toBeNull()
  })

  it('parses a simple command without args', () => {
    expect(parseSlashCommand('/help')).toEqual(['/help', []])
  })

  it('parses a command with plain args', () => {
    expect(parseSlashCommand('/spawn my-slug My Task Title')).toEqual([
      '/spawn',
      ['my-slug', 'My', 'Task', 'Title'],
    ])
  })

  it('parses quoted args as a single token', () => {
    expect(parseSlashCommand('/spawn my-slug "My Task Title"')).toEqual([
      '/spawn',
      ['my-slug', 'My Task Title'],
    ])
  })

  it('normalizes command to lowercase', () => {
    expect(parseSlashCommand('/HELP')).toEqual(['/help', []])
  })

  it('handles extra whitespace between tokens', () => {
    expect(parseSlashCommand('/spawn  slug  desc')).toEqual(['/spawn', ['slug', 'desc']])
  })
})

describe('dispatchSlashCommand', () => {
  it('/help lists all commands', async () => {
    const result = await dispatchSlashCommand('/help', [], {})
    expect(result.ok).toBe(true)
    expect(result.message).toContain('/spawn')
    expect(result.message).toContain('/grant')
    expect(result.message).toContain('/cancel')
  })

  it('unknown command returns ok:false', async () => {
    const result = await dispatchSlashCommand('/unknown', [], {})
    expect(result.ok).toBe(false)
    expect(result.message).toContain('/help')
  })

  it('/spawn validates slug format', async () => {
    const result = await dispatchSlashCommand('/spawn', ['Bad-Slug!', 'desc'], {})
    expect(result.ok).toBe(false)
  })

  it('/spawn rejects missing cwd', async () => {
    const result = await dispatchSlashCommand('/spawn', ['my-slug', 'desc'], {})
    expect(result.ok).toBe(false)
    expect(result.message).toContain('working directory')
  })

  it('/spawn validates missing description', async () => {
    const result = await dispatchSlashCommand('/spawn', ['my-slug'], {})
    expect(result.ok).toBe(false)
  })

  it('/spawn calls POST /api/tasks on success', async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ id: '1' }) })
    vi.stubGlobal('fetch', mockFetch)
    const result = await dispatchSlashCommand('/spawn', ['my-slug', 'My Title'], { cwd: '/repo' })
    expect(result.ok).toBe(true)
    expect(mockFetch).toHaveBeenCalledWith('/api/tasks', expect.objectContaining({ method: 'POST' }))
    vi.unstubAllGlobals()
  })

  it('/grant requires taskId', async () => {
    const result = await dispatchSlashCommand('/grant', ['Bash'], {})
    expect(result.ok).toBe(false)
    expect(result.message).toContain('pipeline task')
  })

  it('/cancel calls cancel endpoint', async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) })
    vi.stubGlobal('fetch', mockFetch)
    await dispatchSlashCommand('/cancel', [], { taskId: 'task-123' })
    expect(mockFetch).toHaveBeenCalledWith('/api/tasks/task-123/cancel', expect.objectContaining({ method: 'POST' }))
    vi.unstubAllGlobals()
  })

  it('/retry calls retry endpoint', async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) })
    vi.stubGlobal('fetch', mockFetch)
    await dispatchSlashCommand('/retry', [], { taskId: 'task-123' })
    expect(mockFetch).toHaveBeenCalledWith('/api/tasks/task-123/retry', expect.objectContaining({ method: 'POST' }))
    vi.unstubAllGlobals()
  })

  it('/promote calls progress endpoint', async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) })
    vi.stubGlobal('fetch', mockFetch)
    await dispatchSlashCommand('/promote', [], { taskId: 'task-123' })
    expect(mockFetch).toHaveBeenCalledWith('/api/tasks/task-123/progress', expect.objectContaining({ method: 'POST' }))
    vi.unstubAllGlobals()
  })

  it('/grant resolves matching permission request', async () => {
    const mockFetch = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: async () => [{ id: 'req-1', tool: 'Bash' }],
      })
      .mockResolvedValueOnce({ ok: true, json: async () => ({}) })
    vi.stubGlobal('fetch', mockFetch)

    const result = await dispatchSlashCommand('/grant', ['Bash'], { taskId: 'task-123' })
    expect(result.ok).toBe(true)
    expect(result.message).toContain('Bash')
    expect(mockFetch).toHaveBeenCalledTimes(2)
    expect(mockFetch).toHaveBeenNthCalledWith(
      2,
      '/api/permission-requests/req-1/resolve',
      expect.objectContaining({ method: 'POST' }),
    )
    vi.unstubAllGlobals()
  })
})

describe('fetchDynamicCommands', () => {
  it('sends sessionId and parses the commands envelope', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        commands: [{ name: '/ship', description: 'Ship it', source: 'user' }],
        engineVersion: '2.1.161',
        builtinsMayBeStale: false,
      }),
    })
    vi.stubGlobal('fetch', mockFetch)

    const set = await fetchDynamicCommands({ sessionId: 'sess-A' })
    expect(mockFetch).toHaveBeenCalledWith('/api/slash-commands?sessionId=sess-A')
    expect(set.commands).toEqual([{ name: '/ship', description: 'Ship it' }])
    expect(set.builtinsMayBeStale).toBe(false)
    vi.unstubAllGlobals()
  })

  it('passes spawnerId and cwd as query params', async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ commands: [] }) })
    vi.stubGlobal('fetch', mockFetch)

    await fetchDynamicCommands({ spawnerId: 'sp-1', cwd: '/repo' })
    const url = mockFetch.mock.calls[0][0] as string
    expect(url).toContain('spawnerId=sp-1')
    expect(url).toContain(`cwd=${encodeURIComponent('/repo')}`)
    vi.unstubAllGlobals()
  })

  it('maps argumentHint onto usage so the menu can show the template', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        commands: [{
          name: '/branch-review',
          description: 'Review a branch',
          source: 'plugin:skills',
          argumentHint: '[base-branch] [--apply-fixes]',
        }],
      }),
    }))

    const set = await fetchDynamicCommands({ sessionId: 'sess-hint' })

    expect(set.commands[0].usage).toBe('[base-branch] [--apply-fixes]')
    vi.unstubAllGlobals()
  })

  it('leaves usage unset when the command declares an empty hint', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        commands: [{ name: '/noargs', description: 'No args', source: 'user', argumentHint: '' }],
      }),
    }))

    const set = await fetchDynamicCommands({ sessionId: 'sess-empty-hint' })

    expect(set.commands[0].usage).toBeUndefined()
    vi.unstubAllGlobals()
  })

  it('returns [] on non-ok response', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 500 }))
    expect(await fetchDynamicCommands({ sessionId: 'sess-err' })).toEqual({ commands: [], builtinsMayBeStale: false })
    vi.unstubAllGlobals()
  })

  it('carries the stale-builtins flag so the menu can warn about missing commands', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        commands: [{ name: '/ship', description: 'Ship it', source: 'user' }],
        engineVersion: '2.1.224',
        builtinsMayBeStale: true,
      }),
    }))

    const set = await fetchDynamicCommands({ sessionId: 'sess-stale' })

    expect(set.builtinsMayBeStale).toBe(true)
    expect(set.engineVersion).toBe('2.1.224')
    vi.unstubAllGlobals()
  })
})
