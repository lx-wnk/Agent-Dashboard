import type { ActiveView } from '../composables/useViewState'

export type NavGroup = 'Monitor' | 'Build'

export interface NavItemConfig {
  view: ActiveView
  label: string
  icon: string
  group: NavGroup
}

export const NAV_GROUPS: NavGroup[] = ['Monitor', 'Build']

export const NAV_ITEMS: NavItemConfig[] = [
  { view: 'dashboard', label: 'Dashboard', icon: '▦', group: 'Monitor' },
  { view: 'workflows', label: 'Workflows', icon: '⤳', group: 'Monitor' },
  { view: 'pipeline', label: 'Pipeline', icon: '▤', group: 'Build' },
  { view: 'cost', label: 'Cost', icon: '◷', group: 'Build' },
  { view: 'config', label: 'Config', icon: '⊞', group: 'Build' },
]

export function viewTitle(view: ActiveView): string {
  return NAV_ITEMS.find(i => i.view === view)?.label ?? 'Dashboard'
}
