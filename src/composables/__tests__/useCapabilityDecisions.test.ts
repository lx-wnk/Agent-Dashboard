import { afterEach, describe, expect, it, vi } from 'vitest'
import { useCapabilityDecisions } from '../useCapabilityDecisions'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

describe('useCapabilityDecisions', () => {
  it('posts an allow decision to the respond endpoint', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 204 })
    vi.stubGlobal('fetch', fetchMock)
    const { resolve } = useCapabilityDecisions()

    const outcome = await resolve('cap-1', 'allow')

    expect(outcome).toEqual({ outcome: 'applied' })
    expect(fetchMock).toHaveBeenCalledOnce()
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/capabilities/decisions/respond')
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body)).toEqual({ id: 'cap-1', decision: 'allow' })
  })

  it('posts a deny decision to the respond endpoint', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 204 })
    vi.stubGlobal('fetch', fetchMock)
    const { resolve } = useCapabilityDecisions()

    await resolve('cap-2', 'deny')

    const [, init] = fetchMock.mock.calls[0]
    expect(JSON.parse(init.body)).toEqual({ id: 'cap-2', decision: 'deny' })
  })

  it('treats a 404 as already-resolved rather than an error', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 404 }))
    const { resolve, resolvingIds } = useCapabilityDecisions()

    const outcome = await resolve('cap-3', 'allow')

    expect(outcome).toEqual({ outcome: 'already-resolved' })
    expect(resolvingIds.value['cap-3']).toBeUndefined()
  })

  it('reports a 500 as an error outcome the caller can act on', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 500, json: async () => ({ error: 'boom' }) }))
    const { resolve, resolvingIds } = useCapabilityDecisions()

    const outcome = await resolve('cap-4', 'allow')

    expect(outcome).toEqual({ outcome: 'error', message: 'boom' })
    expect(resolvingIds.value['cap-4']).toBeUndefined()
  })

  it('reports a network failure as an error outcome and clears the in-flight state', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('network down')))
    const { resolve, resolvingIds } = useCapabilityDecisions()

    const outcome = await resolve('cap-6', 'allow')

    expect(outcome).toEqual({ outcome: 'error', message: 'network down' })
    expect(resolvingIds.value['cap-6']).toBeUndefined()
  })

  it('does not fire a second request while one is in flight for the same id', async () => {
    let resolveFetch!: (v: unknown) => void
    const fetchMock = vi.fn().mockImplementationOnce(() => new Promise((resolve) => {
      resolveFetch = resolve
    }))
    vi.stubGlobal('fetch', fetchMock)
    const { resolve, resolvingIds } = useCapabilityDecisions()

    const first = resolve('cap-5', 'allow')
    expect(resolvingIds.value['cap-5']).toBe(true)

    const second = await resolve('cap-5', 'deny')
    expect(second).toEqual({ outcome: 'in-flight' })
    expect(fetchMock).toHaveBeenCalledOnce()

    resolveFetch({ ok: true, status: 204 })
    await first
    expect(resolvingIds.value['cap-5']).toBeUndefined()
  })
})
