import { afterEach, describe, expect, it, vi } from 'vitest'
import { usePlanReview } from '../usePlanReview'

function mockFetch(impl: (url: string, init?: RequestInit) => Promise<unknown>) {
  vi.stubGlobal('fetch', vi.fn((url: string, init?: RequestInit) =>
    impl(url, init).then(value => ({
      ok: true,
      status: 200,
      json: async () => value,
    })),
  ))
}

function mockFetchOnce(value: unknown, ok = true, status = 200) {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok,
    status,
    json: async () => value,
  }))
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  vi.useRealTimers()
})

describe('usePlanReview.fetchStatus', () => {
  it('fetches GET /api/plan/{id}/status and exposes gate_state and approved_plan', async () => {
    mockFetchOnce({ gate_state: 'awaiting_user', approved_plan: null })

    const pr = usePlanReview(() => 'task-42')
    await pr.fetchStatus()

    expect(pr.gateState.value).toBe('awaiting_user')
    expect(pr.loading.value).toBe(false)
    const [url] = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0]
    expect(url).toBe('/api/plan/task-42/status')
  })

  it('exposes approved_plan when present', async () => {
    mockFetchOnce({ gate_state: 'done', approved_plan: { steps: ['build'] } })

    const pr = usePlanReview(() => 'task-99')
    await pr.fetchStatus()

    expect(pr.gateState.value).toBe('done')
    expect(pr.approvedPlan.value).toEqual({ steps: ['build'] })
  })

  it('no-ops when taskId is null', async () => {
    const fetchSpy = vi.fn()
    vi.stubGlobal('fetch', fetchSpy)

    const pr = usePlanReview(() => null)
    await pr.fetchStatus()

    expect(fetchSpy).not.toHaveBeenCalled()
  })

  it('sets error when fetch returns non-ok', async () => {
    mockFetchOnce({}, false, 500)

    const pr = usePlanReview(() => 'task-1')
    await pr.fetchStatus()

    expect(pr.error.value).toBeTruthy()
  })
})

describe('usePlanReview.approve', () => {
  it('pOSTs to /api/plan/{id}/approve and returns the updated task', async () => {
    const updatedTask = { id: 'task-1', currentStage: 'implementation' }
    mockFetch((url, init) => {
      if (url === '/api/plan/task-1/approve' && init?.method === 'POST')
        return Promise.resolve(updatedTask)
      return Promise.resolve({ gate_state: 'awaiting_user', approved_plan: null })
    })

    const pr = usePlanReview(() => 'task-1')
    const result = await pr.approve()

    expect(result).toMatchObject({ id: 'task-1', currentStage: 'implementation' })
    const calls = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls
    const approveCall = calls.find((call: any[]) => String(call[0]).includes('/approve'))
    expect(approveCall).toBeTruthy()
    expect(approveCall![1]?.method).toBe('POST')
  })

  it('returns null and sets error on failure', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      json: async () => ({ error: 'approve failed' }),
    }))

    const pr = usePlanReview(() => 'task-1')
    const result = await pr.approve()

    expect(result).toBeNull()
    expect(pr.error.value).toBeTruthy()
  })
})

describe('usePlanReview.reject', () => {
  it('pOSTs to /api/plan/{id}/reject with feedback body', async () => {
    const postedBodies: unknown[] = []
    vi.stubGlobal('fetch', vi.fn((url: string, init?: RequestInit) => {
      if (url === '/api/plan/task-1/reject') {
        postedBodies.push(JSON.parse(init?.body as string))
        return Promise.resolve({ ok: true, status: 204, json: async () => ({}) })
      }
      return Promise.resolve({ ok: true, status: 200, json: async () => ({ gate_state: 'awaiting_user' }) })
    }))

    const pr = usePlanReview(() => 'task-1')
    await pr.reject('Please add more detail on step 3')

    expect(postedBodies[0]).toMatchObject({ feedback: 'Please add more detail on step 3' })
  })
})
