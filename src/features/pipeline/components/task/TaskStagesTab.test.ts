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
    stage: 'concept',
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

    const block = wrapper.get('[data-testid="stage-injection-run-1"]')
    expect(block.text()).toContain('2 entries')
    expect(block.text()).toContain('1450 / 2000')
    expect(block.text()).toContain('9 candidates')
  })

  // The route takes exactly one stageRun id and has no bulk form, so the id
  // must reach the query string per run — counting calls alone would pass on a
  // composable that asked for the same run twice.
  it('asks the injections route once per stage run, carrying that run id', async () => {
    const fetchMock = vi.fn(async (_url: string) => new Response('[]', { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    mountTab([makeRun(), makeRun({ id: 'run-2', iteration: 2 })])
    await flushPromises()

    expect(fetchMock.mock.calls.map(c => c[0])).toEqual([
      '/api/memory/injections?stageRun=run-1',
      '/api/memory/injections?stageRun=run-2',
    ])
  })

  it('renders nothing for a stage run that received no memory push', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('[]', { status: 200 })))

    const wrapper = mountTab([makeRun()])
    await flushPromises()

    expect(wrapper.find('[data-testid="stage-injection-run-1"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="stage-injection-denied"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="stage-injection-error"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="stage-injection-loading"]').exists()).toBe(false)
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

  // A refused read and a broken one have different fixes; folding the 500 into
  // the denial notice would send the user to the Grants panel over an outage.
  it('reports a transport failure as an error, not as a denial', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('boom', { status: 500 })))

    const wrapper = mountTab([makeRun()])
    await flushPromises()

    expect(wrapper.get('[data-testid="stage-injection-error"]').text()).toContain('500')
    expect(wrapper.find('[data-testid="stage-injection-denied"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="stage-injection-run-1"]').exists()).toBe(false)
  })

  // Absence is rendered as nothing, so the in-flight window must not look like
  // "this run received no memory push".
  it('says the injections are still being fetched while the requests are in flight', async () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>(() => {})))

    const wrapper = mountTab([makeRun()])
    await flushPromises()

    expect(wrapper.find('[data-testid="stage-injection-loading"]').exists()).toBe(true)
  })

  // Every stage-run refresh starts a new batch of N requests, and the older
  // batch can answer last. Applying it would drop the newer batch's runs and
  // silently show "no memory push" for a run that has one.
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
    expect(resolvers).toHaveLength(3)

    resolvers[1]([makeInjection()])
    resolvers[2]([makeInjection({ id: 'inj-2', stageRunId: 'run-2', entryIds: ['e3'] })])
    await flushPromises()

    // The stale first batch answers only now, and knows nothing of run-2.
    resolvers[0]([])
    await flushPromises()

    expect(wrapper.find('[data-testid="stage-injection-run-2"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="stage-injection-run-1"]').text()).toContain('2 entries')
  })
})
