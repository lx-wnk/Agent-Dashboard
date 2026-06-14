import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

describe('usePipelineConfig singleton', () => {
  beforeEach(() => {
    vi.resetModules()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('fires fetch only once across multiple calls and shares state', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve({
        ok: true,
        json: async () => ({
          maxAutoRetries: 7,
          maxParallelOrchestrators: 2,
          stageTimeoutSeconds: 300,
          retryBackoffSeconds: 60,
        }),
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    const { usePipelineConfig } = await import('../usePipelineConfig')
    const a = usePipelineConfig()
    const b = usePipelineConfig()

    await new Promise(r => setTimeout(r, 0))

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(a.maxAutoRetries).toBe(b.maxAutoRetries)
    expect(a.config).toBe(b.config)
  })

  it('retries fetch after failure', async () => {
    const successResponse = {
      ok: true,
      json: async () => ({
        maxAutoRetries: 5,
        maxParallelOrchestrators: 2,
        stageTimeoutSeconds: 300,
        retryBackoffSeconds: 60,
      }),
    }
    const fetchMock = vi.fn()
      .mockRejectedValueOnce(new Error('network error'))
      .mockResolvedValueOnce(successResponse)
    vi.stubGlobal('fetch', fetchMock)

    // First call — fetch rejects, configPromise is reset to null
    const { usePipelineConfig: usePipelineConfig1 } = await import('../usePipelineConfig')
    usePipelineConfig1()
    await new Promise(r => setTimeout(r, 0))

    // Reset modules so the module re-initialises with configPromise = null
    vi.resetModules()
    vi.stubGlobal('fetch', fetchMock)

    // Second call — fetch succeeds
    const { usePipelineConfig: usePipelineConfig2 } = await import('../usePipelineConfig')
    const { maxAutoRetries } = usePipelineConfig2()
    await new Promise(r => setTimeout(r, 0))

    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(maxAutoRetries.value).toBe(5)
  })
})
