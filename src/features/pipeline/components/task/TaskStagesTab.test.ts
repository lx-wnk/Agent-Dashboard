import type { Ref } from 'vue'
import type { StageRun } from '@/types'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { isRef, ref } from 'vue'
import TaskStagesTab from '@/features/pipeline/components/task/TaskStagesTab.vue'
import { TaskDetailsKey } from '@/features/pipeline/composables/taskModalContext'

vi.mock('@/features/pipeline/components/StageOutputView.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/ui/AppChip.vue', () => ({ default: { template: '<span><slot /></span>' } }))

function makeRun(overrides: Partial<StageRun> = {}): StageRun {
  return {
    id: 'run-1',
    taskId: 'task-1',
    stage: 'backlog',
    sessionId: null,
    sessionName: null,
    pid: null,
    status: 'completed',
    startedAt: '2026-01-01T00:00:00Z',
    endedAt: '2026-01-01T00:05:00Z',
    iteration: 1,
    output: null,
    tokensUsed: 0,
    costCents: 0,
    lastGrantAt: null,
    ...overrides,
  } as StageRun
}

function makeInjection(overrides: Record<string, unknown> = {}) {
  return {
    id: 'inj-1',
    stageRunId: 'run-1',
    entryIds: ['e1', 'e2'],
    charBudget: 2000,
    charsUsed: 1450,
    candidateCount: 9,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

function mountTab(stageRuns: StageRun[] | Ref<StageRun[]>) {
  const runs = isRef(stageRuns) ? stageRuns : ref(stageRuns)
  return mount(TaskStagesTab, {
    attachTo: document.body,
    global: {
      provide: {
        [TaskDetailsKey as symbol]: { stageRuns: runs },
      },
    },
  })
}

describe('taskStagesTab — memory injections', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('shows the injection budget, spend and candidate count for a stage run', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify([makeInjection()]), { status: 200 })))

    const wrapper = mountTab([makeRun()])
    await flushPromises()

    const block = wrapper.get('[data-testid="stage-injection-inj-1"]')
    expect(block.text()).toContain('2 entries')
    expect(block.text()).toContain('1450 / 2000')
    expect(block.text()).toContain('9 candidates')
  })

  // Gate.Authorize records a rate-limit use per call against the same
  // memory.read grant the pipeline's memory push spends, so N runs must cost
  // exactly one request — and every run id must still reach the query string,
  // which counting calls alone would not show.
  it('asks the injections route once for the whole tab, carrying every run id', async () => {
    const fetchMock = vi.fn(async (_url: string) => new Response('[]', { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    mountTab([makeRun(), makeRun({ id: 'run-2', iteration: 2 })])
    await flushPromises()

    expect(fetchMock.mock.calls.map(c => c[0])).toEqual([
      '/api/memory/injections?stageRun=run-1&stageRun=run-2',
    ])
  })

  it('splits one bulk response back onto the run each record belongs to', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify([
      makeInjection(),
      makeInjection({ id: 'inj-2', stageRunId: 'run-2', entryIds: ['e3'], charsUsed: 300, candidateCount: 4 }),
    ]), { status: 200 })))

    const wrapper = mountTab([makeRun(), makeRun({ id: 'run-2', iteration: 2 })])
    await flushPromises()

    expect(wrapper.get('[data-testid="stage-injection-inj-1"]').text()).toContain('2 entries')
    expect(wrapper.get('[data-testid="stage-injection-inj-2"]').text()).toContain('1 entries')
    expect(wrapper.find('[data-testid="stage-injection-none-run-1"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="stage-injection-none-run-2"]').exists()).toBe(false)
  })

  // A blank row is indistinguishable from "this tab does not show memory
  // pushes at all", so the run that received none says so itself.
  it('says so on the run that received no memory push', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify([makeInjection()]), { status: 200 })))

    const wrapper = mountTab([makeRun(), makeRun({ id: 'run-2', iteration: 2 })])
    await flushPromises()

    expect(wrapper.find('[data-testid="stage-injection-none-run-1"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="stage-injection-none-run-2"]').text()).toBe('no memory push')
    expect(wrapper.find('[data-testid="stage-injection-denied"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="stage-injection-error"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="stage-injection-loading"]').exists()).toBe(false)
  })

  // The tab-level notices already name the cause; repeating "no memory push"
  // under every run would claim the pushes are known to be absent when the
  // read never landed.
  it('stays quiet per run while loading and under a tab-level notice', async () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>(() => {})))
    const pending = mountTab([makeRun()])
    await flushPromises()
    expect(pending.find('[data-testid="stage-injection-none-run-1"]').exists()).toBe(false)

    vi.stubGlobal('fetch', vi.fn(async () => new Response('{}', { status: 403 })))
    const refused = mountTab([makeRun()])
    await flushPromises()
    expect(refused.find('[data-testid="stage-injection-none-run-1"]').exists()).toBe(false)

    vi.stubGlobal('fetch', vi.fn(async () => new Response('boom', { status: 500 })))
    const broken = mountTab([makeRun()])
    await flushPromises()
    expect(broken.find('[data-testid="stage-injection-none-run-1"]').exists()).toBe(false)
  })

  it('shows one denial notice for the whole tab, naming the global scope', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: 'capability memory.read denied' }), { status: 403 })))

    const wrapper = mountTab([makeRun(), makeRun({ id: 'run-2', iteration: 2 })])
    await flushPromises()

    const notices = wrapper.findAll('[data-testid="stage-injection-denied"]')
    expect(notices).toHaveLength(1)
    expect(notices[0].text()).toContain('global')
    expect(wrapper.find('[data-testid="stage-injection-error"]').exists()).toBe(false)
  })

  // handler.go answers 403 for every Gate.Authorize error — a missing grant, a
  // rate limit, an unanswered ask, a failed read of the grant store. Only the
  // server knows which, so its message leads and the grant explanation is
  // demoted to a cause the notice merely suspects.
  it('leads the denial with the server message and demotes the global-scope cause', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: 'rate limit exceeded for memory.read' }), { status: 403 })))

    const wrapper = mountTab([makeRun()])
    await flushPromises()

    const notice = wrapper.get('[data-testid="stage-injection-denied"]')
    expect(notice.get('strong').text()).toBe('rate limit exceeded for memory.read')
    expect(notice.text().indexOf('rate limit exceeded for memory.read')).toBeLessThan(notice.text().indexOf('Most likely cause'))
    expect(notice.attributes('role')).toBe('alert')
  })

  // The fallback exists for a 403 carrying no message of its own; it must not
  // repeat the cause sentence the notice already prints below it.
  it('does not state the global-scope cause twice when the refusal carries no message', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('{}', { status: 403 })))

    const wrapper = mountTab([makeRun()])
    await flushPromises()

    const text = wrapper.get('[data-testid="stage-injection-denied"]').text()
    expect(text).toContain('refused this read (HTTP 403)')
    expect(text.split('global scope')).toHaveLength(2)
  })

  // A denial that outlives the grant which fixed it tells the user the grant
  // did not work.
  it('clears the denial once a later batch is allowed through', async () => {
    const fetchMock = vi.fn(async (_url: string) => new Response(JSON.stringify({ error: 'capability memory.read denied' }), { status: 403 }))
    vi.stubGlobal('fetch', fetchMock)

    const runs = ref([makeRun()])
    const wrapper = mountTab(runs)
    await flushPromises()
    expect(wrapper.find('[data-testid="stage-injection-denied"]').exists()).toBe(true)

    fetchMock.mockImplementation(async () => new Response(JSON.stringify([makeInjection()]), { status: 200 }))
    runs.value = [makeRun()]
    await flushPromises()

    expect(wrapper.find('[data-testid="stage-injection-denied"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="stage-injection-inj-1"]').exists()).toBe(true)
  })

  // Same reasoning for the error banner: an outage that has ended must not keep
  // reporting itself over a batch that succeeded.
  it('clears the error once a later batch succeeds', async () => {
    const fetchMock = vi.fn(async (_url: string) => new Response('boom', { status: 500 }))
    vi.stubGlobal('fetch', fetchMock)

    const runs = ref([makeRun()])
    const wrapper = mountTab(runs)
    await flushPromises()
    expect(wrapper.find('[data-testid="stage-injection-error"]').exists()).toBe(true)

    fetchMock.mockImplementation(async () => new Response(JSON.stringify([makeInjection()]), { status: 200 }))
    runs.value = [makeRun()]
    await flushPromises()

    expect(wrapper.find('[data-testid="stage-injection-error"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="stage-injection-inj-1"]').exists()).toBe(true)
  })

  // A resumed or requeued run is spawned again on the same row and gains a
  // second injection (RecordInjection creates, never upserts), so a testid
  // keyed on the run would put two elements behind one selector.
  it('addresses each injection of a re-spawned run separately', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify([
      makeInjection(),
      makeInjection({ id: 'inj-2', entryIds: ['e3'], charsUsed: 300, candidateCount: 4 }),
    ]), { status: 200 })))

    const wrapper = mountTab([makeRun()])
    await flushPromises()

    expect(wrapper.findAll('[data-testid="stage-injection-inj-1"]')).toHaveLength(1)
    expect(wrapper.get('[data-testid="stage-injection-inj-2"]').text()).toContain('1 entries')
  })

  // Every other render test mounts a single stage run, under which rendering
  // Object.values(byStageRun).flat() is indistinguishable from the real
  // byStageRun[run.id] — so "every run's memory push shows under every run"
  // goes undetected. Two runs, one injection each, is what makes it visible.
  it('renders under each run only that run\'s own memory push', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify([
      makeInjection(),
      makeInjection({ id: 'inj-2', stageRunId: 'run-2', entryIds: ['e3'], charsUsed: 300, candidateCount: 4 }),
    ]), { status: 200 })))

    const wrapper = mountTab([makeRun(), makeRun({ id: 'run-2', iteration: 2 })])
    await flushPromises()

    const first = wrapper.get('[data-testid="stage-run-run-1"]')
    expect(first.find('[data-testid="stage-injection-inj-1"]').exists()).toBe(true)
    expect(first.find('[data-testid="stage-injection-inj-2"]').exists()).toBe(false)

    const second = wrapper.get('[data-testid="stage-run-run-2"]')
    expect(second.find('[data-testid="stage-injection-inj-2"]').exists()).toBe(true)
    expect(second.find('[data-testid="stage-injection-inj-1"]').exists()).toBe(false)
  })

  // No run means no request was issued, so "checking" would be a lie — and the
  // first paint happens before any response could arrive.
  it('does not claim to be checking when there is no stage run to ask about', () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>(() => {})))

    const wrapper = mountTab([])

    expect(wrapper.find('[data-testid="stage-injection-loading"]').exists()).toBe(false)
  })

  // A refused read and a broken one have different fixes; folding the 500 into
  // the denial notice would send the user to the Grants panel over an outage.
  it('reports a transport failure as an error, not as a denial', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('boom', { status: 500 })))

    const wrapper = mountTab([makeRun()])
    await flushPromises()

    expect(wrapper.get('[data-testid="stage-injection-error"]').text()).toContain('500')
    expect(wrapper.find('[data-testid="stage-injection-denied"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid^="stage-injection-inj"]').exists()).toBe(false)
  })

  // Absence is rendered as nothing, so the in-flight window must not look like
  // "this run received no memory push".
  it('says the injections are still being fetched while the requests are in flight', async () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>(() => {})))

    const wrapper = mountTab([makeRun()])
    await flushPromises()

    expect(wrapper.find('[data-testid="stage-injection-loading"]').exists()).toBe(true)
  })

  // Every stage-run refresh starts a new request, and the older one can answer
  // last. Applying it would drop the newer batch's runs and silently show
  // "no memory push" for a run that has one.
  it('keeps the newest batch when an older one resolves last', async () => {
    const resolvers: Array<(rows: unknown[]) => void> = []
    vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>((resolve) => {
      resolvers.push(rows => resolve(new Response(JSON.stringify(rows), { status: 200 })))
    })))

    const runs = ref([makeRun()])
    const wrapper = mountTab(runs)
    await flushPromises()
    expect(resolvers).toHaveLength(1)

    runs.value = [makeRun(), makeRun({ id: 'run-2', iteration: 2 })]
    await flushPromises()
    expect(resolvers).toHaveLength(2)

    resolvers[1]([makeInjection(), makeInjection({ id: 'inj-2', stageRunId: 'run-2', entryIds: ['e3'] })])
    await flushPromises()

    // The stale first request answers only now, and knows nothing of run-2.
    resolvers[0]([])
    await flushPromises()

    expect(wrapper.find('[data-testid="stage-injection-inj-2"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="stage-injection-inj-1"]').text()).toContain('2 entries')
  })
})
