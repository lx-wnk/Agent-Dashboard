import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { usePromptTemplates } from '../usePromptTemplates'

function makeTemplate(overrides: Partial<{ id: string, name: string, body: string, createdAt: string }> = {}) {
  return { id: 't1', name: 'Greeting', body: 'Hello', createdAt: '2026-01-01T00:00:00Z', ...overrides }
}

beforeEach(() => {
  vi.stubGlobal('location', { origin: 'http://127.0.0.1:13120' })
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('usePromptTemplates', () => {
  it('fetches templates immediately on creation', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve([makeTemplate()]) })
    vi.stubGlobal('fetch', fetchMock)

    const { templates } = usePromptTemplates()
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith('/api/prompt-templates')
    expect(templates.value).toEqual([makeTemplate()])
  })

  it('leaves templates empty when the initial fetch fails', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 500 }))

    const { templates } = usePromptTemplates()
    await flushPromises()

    expect(templates.value).toEqual([])
  })

  it('create() POSTs the new template and reloads the list', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve([]) })
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(makeTemplate()) })
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve([makeTemplate()]) })
    vi.stubGlobal('fetch', fetchMock)

    const { templates, create } = usePromptTemplates()
    await flushPromises()

    await create('Greeting', 'Hello')

    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/prompt-templates', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Origin': 'http://127.0.0.1:13120' },
      body: JSON.stringify({ name: 'Greeting', body: 'Hello' }),
    })
    expect(fetchMock).toHaveBeenCalledTimes(3)
    expect(templates.value).toEqual([makeTemplate()])
  })

  it('create() throws the response body and does not reload on failure', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve([]) })
      .mockResolvedValueOnce({ ok: false, text: () => Promise.resolve('name already exists') })
    vi.stubGlobal('fetch', fetchMock)

    const { create } = usePromptTemplates()
    await flushPromises()

    await expect(create('dup', 'body')).rejects.toThrow('name already exists')
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('remove() DELETEs and reloads even when the response is not ok', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve([makeTemplate()]) })
      .mockResolvedValueOnce({ ok: false })
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve([]) })
    vi.stubGlobal('fetch', fetchMock)

    const { templates, remove } = usePromptTemplates()
    await flushPromises()
    expect(templates.value).toHaveLength(1)

    await remove('t1')

    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/prompt-templates/t1', {
      method: 'DELETE',
      headers: { Origin: 'http://127.0.0.1:13120' },
    })
    expect(fetchMock).toHaveBeenCalledTimes(3)
    expect(templates.value).toEqual([])
  })
})
