import type { WorkflowsFilters } from '@/features/workflows/composables/useWorkflows'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick, ref } from 'vue'
import { defaultWorkflowsFilters, useWorkflows } from '@/features/workflows/composables/useWorkflows'

interface JsonInit {
  body?: unknown
  status?: number
  delayMs?: number
  signal?: AbortSignal
}

function flush(): Promise<void> {
  return new Promise(resolve => setTimeout(resolve, 0))
}

function jsonResponse(init: JsonInit = {}): Response {
  const body = JSON.stringify(init.body ?? {})
  return new Response(body, {
    status: init.status ?? 200,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('useWorkflows', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('fetches the default tab on mount', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(async () =>
      jsonResponse({ body: { nodes: [], links: [], meta: { sessionCount: 0, callCount: 0 } } }),
    )
    const filters = ref<WorkflowsFilters>({})
    const wf = useWorkflows(filters)
    await flush()
    expect(fetchSpy).toHaveBeenCalledTimes(1)
    expect(fetchSpy.mock.calls[0][0]).toContain('/api/visualizations/sankey')
    expect(wf.sankey.data).toEqual({ nodes: [], links: [], meta: { sessionCount: 0, callCount: 0 } })
    expect(wf.sankey.loading).toBe(false)
    expect(wf.sankey.error).toBeNull()
  })

  it('skips DAG fetch when sessionId is missing', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(async () => jsonResponse())
    const filters = ref<WorkflowsFilters>({})
    const wf = useWorkflows(filters)
    await flush()
    wf.setActiveTab('dag')
    await flush()
    // Only the initial sankey fetch should have happened; DAG must short-circuit.
    expect(fetchSpy.mock.calls.filter(c => String(c[0]).includes('/dag')).length).toBe(0)
    expect(wf.dag.error).toBeNull()
    expect(wf.dag.loading).toBe(false)
  })

  it('aborts the previous request when filters change', async () => {
    const seenSignals: (AbortSignal | undefined)[] = []
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (_url, init) => {
      seenSignals.push(init?.signal as AbortSignal | undefined)
      await new Promise(r => setTimeout(r, 50))
      if ((init?.signal as AbortSignal)?.aborted)
        throw Object.assign(new Error('aborted'), { name: 'AbortError' })
      return jsonResponse({ body: { nodes: [], links: [], meta: { sessionCount: 0, callCount: 0 } } })
    })
    const filters = ref<WorkflowsFilters>({})
    useWorkflows(filters)
    await nextTick()
    filters.value = { from: '2026-05-01T00:00:00Z' }
    await nextTick()
    await flush()
    // The first request's signal must be aborted, the second still pending or done.
    expect(seenSignals[0]?.aborted).toBe(true)
  })

  it('converts datetime-local bounds to ISO/UTC in the query string', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(async () =>
      jsonResponse({ body: { nodes: [], links: [], meta: { sessionCount: 0, callCount: 0 } } }),
    )
    // datetime-local emits local wall-clock with no timezone.
    const filters = ref<WorkflowsFilters>({ from: '2026-05-01T14:30', to: '2026-05-08T14:30' })
    useWorkflows(filters)
    await flush()
    const url = String(fetchSpy.mock.calls[0][0])
    // The raw datetime-local value must not reach the server verbatim.
    expect(url).not.toContain('2026-05-01T14%3A30&')
    // It must be serialized as a full ISO timestamp (ends in Z after encoding).
    const parsed = new URL(url, 'http://localhost')
    expect(parsed.searchParams.get('from')).toBe(new Date('2026-05-01T14:30').toISOString())
    expect(parsed.searchParams.get('to')).toBe(new Date('2026-05-08T14:30').toISOString())
  })

  it('defaultWorkflowsFilters spans the last 7 days in datetime-local format', () => {
    const f = defaultWorkflowsFilters()
    expect(f.from).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/)
    expect(f.to).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/)
    const span = new Date(f.to!).getTime() - new Date(f.from!).getTime()
    expect(span).toBe(7 * 24 * 60 * 60 * 1000)
  })

  it('surfaces server error messages', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async () =>
      jsonResponse({ body: { error: 'session query parameter is required for dag' }, status: 400 }),
    )
    const filters = ref<WorkflowsFilters>({ sessionId: 'abc' })
    const wf = useWorkflows(filters)
    wf.setActiveTab('dag')
    await flush()
    await flush()
    expect(wf.dag.error).toBe('session query parameter is required for dag')
  })
})
