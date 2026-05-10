import { describe, expect, it, vi } from 'vitest'
import { dispatchSlashCommand, parseSlashCommand } from './useSlashCommands'

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
    expect(result.message).toContain('Arbeitsverzeichnis')
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
    expect(result.message).toContain('Pipeline-Task')
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
