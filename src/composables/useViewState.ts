import type { AgentGroup, AgentSort } from '../utils/agentGroup'
import { ref, watch } from 'vue'

export type ActiveView = 'dashboard' | 'workflows' | 'pipeline' | 'cost'
export type DashboardLayout = 'cards' | 'list'

const ACTIVE_VIEWS: ActiveView[] = ['dashboard', 'workflows', 'pipeline', 'cost']
const AGENT_SORT_VALUES: AgentSort[] = ['latest', 'longest', 'expensive']
const AGENT_GROUP_VALUES: AgentGroup[] = ['none', 'status', 'model']

function readInitial(): { view: ActiveView, layout: DashboardLayout } {
  const ls = typeof localStorage !== 'undefined' ? localStorage : null
  const stored = ls?.getItem('agent-active-view')
  const storedLayout = ls?.getItem('agent-dashboard-layout')

  const legacy = ls?.getItem('agent-view-mode')
  let view: ActiveView
  let layout: DashboardLayout = storedLayout === 'list' ? 'list' : 'cards'

  if (stored && ACTIVE_VIEWS.includes(stored as ActiveView)) {
    view = stored as ActiveView
  }
  else {
    view = 'dashboard'
    if (stored) {
      // stored value is no longer valid — overwrite to stop the guard firing every load
      ls?.setItem('agent-active-view', 'dashboard')
    }

    if (!stored && legacy) {
      switch (legacy) {
        case 'pipeline':
          view = 'pipeline'
          break
        case 'workflows':
          view = 'workflows'
          break
        case 'config-explorer':
          // Config view was folded into Settings → Spawners → Details; send
          // legacy deep-links to the dashboard rather than a dead view.
          view = 'dashboard'
          break
        case 'cost-analytics':
          view = 'cost'
          break
        case 'list':
          view = 'dashboard'
          layout = 'list'
          break
        case 'cards':
          view = 'dashboard'
          layout = 'cards'
          break
      }
      ls?.removeItem('agent-view-mode')
    }
  }
  return { view, layout }
}

function readStoredSort(): AgentSort {
  const ls = typeof localStorage !== 'undefined' ? localStorage : null
  const v = ls?.getItem('agent-dashboard-sort')
  return v && AGENT_SORT_VALUES.includes(v as AgentSort) ? (v as AgentSort) : 'latest'
}

function readStoredGroup(): AgentGroup {
  const ls = typeof localStorage !== 'undefined' ? localStorage : null
  const v = ls?.getItem('agent-dashboard-group')
  return v && AGENT_GROUP_VALUES.includes(v as AgentGroup) ? (v as AgentGroup) : 'none'
}

function readStoredProject(): string {
  const ls = typeof localStorage !== 'undefined' ? localStorage : null
  return ls?.getItem('agent-dashboard-project') ?? 'all'
}

const initial = readInitial()
const activeView = ref<ActiveView>(initial.view)
const dashboardLayout = ref<DashboardLayout>(initial.layout)
const dashboardSort = ref<AgentSort>(readStoredSort())
const dashboardGroup = ref<AgentGroup>(readStoredGroup())
const dashboardProject = ref<string>(readStoredProject())

watch(activeView, (v) => {
  if (typeof localStorage !== 'undefined')
    localStorage.setItem('agent-active-view', v)
}, { flush: 'sync' })
watch(dashboardLayout, (l) => {
  if (typeof localStorage !== 'undefined')
    localStorage.setItem('agent-dashboard-layout', l)
}, { flush: 'sync' })
watch(dashboardSort, (v) => {
  if (typeof localStorage !== 'undefined')
    localStorage.setItem('agent-dashboard-sort', v)
}, { flush: 'sync' })
watch(dashboardGroup, (v) => {
  if (typeof localStorage !== 'undefined')
    localStorage.setItem('agent-dashboard-group', v)
}, { flush: 'sync' })
watch(dashboardProject, (v) => {
  if (typeof localStorage !== 'undefined')
    localStorage.setItem('agent-dashboard-project', v)
}, { flush: 'sync' })

export function useViewState() {
  return { activeView, dashboardLayout, dashboardSort, dashboardGroup, dashboardProject }
}
