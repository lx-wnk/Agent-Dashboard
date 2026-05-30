import { ref, watch } from 'vue'

export type ActiveView = 'dashboard' | 'workflows' | 'pipeline' | 'cost' | 'config'
export type DashboardLayout = 'cards' | 'list'

const ACTIVE_VIEWS: ActiveView[] = ['dashboard', 'workflows', 'pipeline', 'cost', 'config']

function readInitial(): { view: ActiveView; layout: DashboardLayout } {
  const ls = typeof localStorage !== 'undefined' ? localStorage : null
  const stored = ls?.getItem('agent-active-view') as ActiveView | null
  const storedLayout = ls?.getItem('agent-dashboard-layout') as DashboardLayout | null

  const legacy = ls?.getItem('agent-view-mode')
  let view: ActiveView = stored && ACTIVE_VIEWS.includes(stored) ? stored : 'dashboard'
  let layout: DashboardLayout = storedLayout === 'list' ? 'list' : 'cards'

  if (!stored && legacy) {
    switch (legacy) {
      case 'pipeline':
        view = 'pipeline'
        break
      case 'workflows':
        view = 'workflows'
        break
      case 'config-explorer':
        view = 'config'
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
  }
  return { view, layout }
}

const initial = readInitial()
const activeView = ref<ActiveView>(initial.view)
const dashboardLayout = ref<DashboardLayout>(initial.layout)

watch(activeView, (v) => {
  if (typeof localStorage !== 'undefined') localStorage.setItem('agent-active-view', v)
}, { flush: 'sync' })
watch(dashboardLayout, (l) => {
  if (typeof localStorage !== 'undefined') localStorage.setItem('agent-dashboard-layout', l)
}, { flush: 'sync' })

export function useViewState() {
  return { activeView, dashboardLayout }
}
