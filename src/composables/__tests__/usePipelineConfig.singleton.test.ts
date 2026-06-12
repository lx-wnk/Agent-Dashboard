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
})
