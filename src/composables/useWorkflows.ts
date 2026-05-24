import type { Ref } from 'vue'
import type { CoOccurrenceData, DAGData, SankeyData, SpawnTreeData } from '../sdk.generated'
import { getCurrentScope, onScopeDispose, reactive, shallowRef, watch } from 'vue'

export interface WorkflowsFilters {
  sessionId?: string
  from?: string
  to?: string
}

export type WorkflowTab = 'sankey' | 'dag' | 'spawnTree' | 'coOccurrence'

export interface AsyncRef<T> {
  data: T | null
  loading: boolean
  error: string | null
}

export interface UseWorkflowsReturn {
  sankey: AsyncRef<SankeyData>
  dag: AsyncRef<DAGData>
  spawnTree: AsyncRef<SpawnTreeData>
  coOccurrence: AsyncRef<CoOccurrenceData>
  activeTab: Ref<WorkflowTab>
  setActiveTab: (tab: WorkflowTab) => void
  refresh: (tab?: WorkflowTab) => Promise<void>
}

interface Endpoint {
  tab: WorkflowTab
  path: string
  // requiresSession blocks the fetch when filters.sessionId is empty, used
  // by the DAG endpoint which always 400s without one.
  requiresSession: boolean
}

const ENDPOINTS: Record<WorkflowTab, Endpoint> = {
  sankey: { tab: 'sankey', path: '/api/visualizations/sankey', requiresSession: false },
  dag: { tab: 'dag', path: '/api/visualizations/dag', requiresSession: true },
  spawnTree: { tab: 'spawnTree', path: '/api/visualizations/spawn-tree', requiresSession: false },
  coOccurrence: { tab: 'coOccurrence', path: '/api/visualizations/co-occurrence', requiresSession: false },
}

function buildURL(endpoint: Endpoint, filters: WorkflowsFilters): string {
  const params = new URLSearchParams()
  if (filters.sessionId)
    params.set('session', filters.sessionId)
  if (filters.from)
    params.set('from', filters.from)
  if (filters.to)
    params.set('to', filters.to)
  const qs = params.toString()
  return qs ? `${endpoint.path}?${qs}` : endpoint.path
}

export function useWorkflows(filters: Ref<WorkflowsFilters>): UseWorkflowsReturn {
  const sankey = reactive<AsyncRef<SankeyData>>({ data: null, loading: false, error: null })
  const dag = reactive<AsyncRef<DAGData>>({ data: null, loading: false, error: null })
  const spawnTree = reactive<AsyncRef<SpawnTreeData>>({ data: null, loading: false, error: null })
  const coOccurrence = reactive<AsyncRef<CoOccurrenceData>>({ data: null, loading: false, error: null })

  const state: Record<WorkflowTab, AsyncRef<unknown>> = {
    sankey: sankey as AsyncRef<unknown>,
    dag: dag as AsyncRef<unknown>,
    spawnTree: spawnTree as AsyncRef<unknown>,
    coOccurrence: coOccurrence as AsyncRef<unknown>,
  }

  const controllers: Record<WorkflowTab, AbortController | null> = {
    sankey: null,
    dag: null,
    spawnTree: null,
    coOccurrence: null,
  }

  const activeTab = shallowRef<WorkflowTab>('sankey')

  function abortAll(): void {
    for (const tab of Object.keys(controllers) as WorkflowTab[]) {
      controllers[tab]?.abort()
      controllers[tab] = null
    }
  }

  async function fetchTab(tab: WorkflowTab): Promise<void> {
    const endpoint = ENDPOINTS[tab]
    const slot = state[tab]
    if (endpoint.requiresSession && !filters.value.sessionId) {
      slot.data = null
      slot.error = null
      slot.loading = false
      return
    }
    controllers[tab]?.abort()
    const controller = new AbortController()
    controllers[tab] = controller
    slot.loading = true
    slot.error = null
    try {
      const res = await fetch(buildURL(endpoint, filters.value), { signal: controller.signal })
      if (!res.ok) {
        let message = `HTTP ${res.status}`
        try {
          const body = await res.json() as { error?: string }
          if (body?.error)
            message = body.error
        }
        catch {
          // ignore parse failures
        }
        throw new Error(message)
      }
      slot.data = await res.json() as unknown
    }
    catch (err) {
      if ((err as Error).name === 'AbortError')
        return
      slot.error = (err as Error).message
    }
    finally {
      // Only clear loading if this controller is still the active one for the tab.
      if (controllers[tab] === controller) {
        slot.loading = false
        controllers[tab] = null
      }
    }
  }

  async function refresh(tab?: WorkflowTab): Promise<void> {
    if (tab) {
      await fetchTab(tab)
      return
    }
    await fetchTab(activeTab.value)
  }

  function setActiveTab(tab: WorkflowTab): void {
    if (tab === activeTab.value)
      return
    activeTab.value = tab
    void fetchTab(tab)
  }

  watch(filters, () => {
    abortAll()
    void fetchTab(activeTab.value)
  }, { deep: true })

  // Abort all outstanding fetches when the owning component unmounts so
  // their .then/.catch handlers don't write into reactive state that the
  // consumer has already torn down. Only registers when called inside a
  // component setup (getCurrentScope() is non-null).
  if (getCurrentScope())
    onScopeDispose(abortAll)

  // Kick off the first fetch on the default tab.
  void fetchTab(activeTab.value)

  return {
    sankey,
    dag,
    spawnTree,
    coOccurrence,
    activeTab,
    setActiveTab,
    refresh,
  }
}
