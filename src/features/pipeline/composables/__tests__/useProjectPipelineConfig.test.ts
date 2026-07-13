import type { ProjectPipelineConfig } from '@/features/pipeline/composables/useProjectPipelineConfig'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useProjectPipelineConfig } from '@/features/pipeline/composables/useProjectPipelineConfig'

function makeConfig(overrides: Partial<ProjectPipelineConfig> = {}): ProjectPipelineConfig {
  return {
    stageModels: { implementation: 'sonnet', self_review: 'sonnet', finalization: 'sonnet' },
    stageSpawners: { implementation: 's1', self_review: 's1', finalization: 's1' },
    ...overrides,
  }
}

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn())
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useProjectPipelineConfig.fetch', () => {
  it('loads the config for a project and clears loading/error', async () => {
    vi.mocked(fetch).mockResolvedValue({ ok: true, json: async () => makeConfig() } as Response)
    const { config, loading, error, fetch: load } = useProjectPipelineConfig()

    const pending = load('proj-1')
    expect(loading.value).toBe(true)
    await pending

    expect(fetch).toHaveBeenCalledWith('/api/projects/proj-1/pipeline-config')
    expect(config.value).toEqual(makeConfig())
    expect(loading.value).toBe(false)
    expect(error.value).toBeNull()
  })

  it('sets an HTTP-status error when the response is not ok', async () => {
    vi.mocked(fetch).mockResolvedValue({ ok: false, status: 404 } as Response)
    const { config, error, fetch: load } = useProjectPipelineConfig()

    await load('missing-project')

    expect(error.value).toBe('HTTP 404')
    expect(config.value).toBeNull()
  })

  it('sets the network error message when fetch rejects', async () => {
    vi.mocked(fetch).mockRejectedValue(new Error('offline'))
    const { error, fetch: load } = useProjectPipelineConfig()

    await load('proj-1')

    expect(error.value).toBe('offline')
  })

  it('clears a previous error on a subsequent successful load', async () => {
    vi.mocked(fetch).mockResolvedValueOnce({ ok: false, status: 500 } as Response)
    const { config, error, fetch: load } = useProjectPipelineConfig()
    await load('proj-1')
    expect(error.value).toBe('HTTP 500')

    vi.mocked(fetch).mockResolvedValueOnce({ ok: true, json: async () => makeConfig() } as Response)
    await load('proj-1')

    expect(error.value).toBeNull()
    expect(config.value).toEqual(makeConfig())
  })
})

describe('useProjectPipelineConfig.save', () => {
  it('pUTs the partial config and stores the returned full config', async () => {
    const saved = makeConfig({ stageModels: { implementation: 'opus', self_review: 'sonnet', finalization: 'sonnet' } })
    vi.mocked(fetch).mockResolvedValue({ ok: true, json: async () => saved } as Response)
    const { config, save } = useProjectPipelineConfig()

    await save('proj-1', { stageModels: { implementation: 'opus' } })

    expect(fetch).toHaveBeenCalledWith('/api/projects/proj-1/pipeline-config', expect.objectContaining({
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ stageModels: { implementation: 'opus' } }),
    }))
    expect(config.value).toEqual(saved)
  })

  it('surfaces the server error message on a failed save', async () => {
    vi.mocked(fetch).mockResolvedValue({ ok: false, status: 400, json: async () => ({ error: 'invalid model' }) } as Response)
    const { error, save } = useProjectPipelineConfig()

    await save('proj-1', {})

    expect(error.value).toBe('invalid model')
  })

  it('falls back to an HTTP-status message when the error body has no message', async () => {
    vi.mocked(fetch).mockResolvedValue({ ok: false, status: 500, json: async () => ({}) } as Response)
    const { error, save } = useProjectPipelineConfig()

    await save('proj-1', {})

    expect(error.value).toBe('HTTP 500')
  })

  it('falls back to an HTTP-status message when the error body cannot be parsed', async () => {
    vi.mocked(fetch).mockResolvedValue({ ok: false, status: 502, json: () => Promise.reject(new Error('bad json')) } as unknown as Response)
    const { error, save } = useProjectPipelineConfig()

    await save('proj-1', {})

    expect(error.value).toBe('HTTP 502')
  })

  it('surfaces the network error message when the PUT rejects', async () => {
    vi.mocked(fetch).mockRejectedValue(new Error('timeout'))
    const { error, save } = useProjectPipelineConfig()

    await save('proj-1', {})

    expect(error.value).toBe('timeout')
  })
})
