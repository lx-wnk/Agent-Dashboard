import { beforeEach, describe, expect, it, vi } from 'vitest'
import { DEFAULT_LAYOUT, serializeLayout } from './layout'

function settingsResponse(value: string) {
  return new Response(JSON.stringify([{ key: 'workspace.layout', value }]), { status: 200 })
}

async function fresh() {
  vi.resetModules()
  return (await import('./useWorkspace')).useWorkspace()
}

describe('useWorkspace', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('shows the built-in layout when nothing is stored', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(settingsResponse(''))
    const ws = await fresh()
    await ws.load()
    expect(ws.layout.value).toEqual(DEFAULT_LAYOUT)
    expect(ws.locked.value).toBeNull()
  })

  // A parse bug must not destroy what the operator built: editing locks, so
  // nothing writes over the stored value until they choose Reset.
  it('locks editing when the stored layout is unreadable', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(settingsResponse('{broken'))
    const ws = await fresh()
    await ws.load()
    expect(ws.layout.value).toEqual(DEFAULT_LAYOUT)
    expect(ws.locked.value).toMatch(/could not be read/i)
  })

  it('saves with a PATCH and keeps the change on screen when the save fails', async () => {
    const fetch = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(settingsResponse(''))
      .mockResolvedValueOnce(new Response('{"error":"bad request"}', { status: 400 }))
    const ws = await fresh()
    await ws.load()
    const next = { ...DEFAULT_LAYOUT, pages: [{ ...DEFAULT_LAYOUT.pages[0], tiles: [] }] }
    await ws.save(next)
    expect(fetch).toHaveBeenLastCalledWith('/api/settings/workspace.layout', expect.objectContaining({
      method: 'PATCH',
      body: JSON.stringify({ value: serializeLayout(next) }),
    }))
    expect(ws.layout.value).toEqual(next)
    expect(ws.saveError.value).toMatch(/not saved/i)
  })

  it('refuses to save while locked', async () => {
    const fetch = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(settingsResponse('{broken'))
    const ws = await fresh()
    await ws.load()
    await ws.save(DEFAULT_LAYOUT)
    expect(fetch).toHaveBeenCalledTimes(1)
  })

  it('locks editing when the settings endpoint returns a non-ok response', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(new Response('', { status: 500 }))
    const ws = await fresh()
    await ws.load()
    expect(ws.layout.value).toEqual(DEFAULT_LAYOUT)
    expect(ws.locked.value).toMatch(/could not be loaded/i)
  })

  it('locks editing when the settings endpoint rejects (network error)', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new Error('network error'))
    const ws = await fresh()
    await ws.load()
    expect(ws.layout.value).toEqual(DEFAULT_LAYOUT)
    expect(ws.locked.value).toMatch(/could not be loaded/i)
  })
})
