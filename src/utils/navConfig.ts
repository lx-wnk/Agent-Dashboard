import type { ActiveView } from '../composables/useViewState'

export type NavGroup = 'Monitor' | 'Build' | 'Insights'

export interface NavItemConfig {
  view: ActiveView
  label: string
  icon: string
  group: NavGroup
}

export const NAV_GROUPS: NavGroup[] = ['Monitor', 'Build', 'Insights']

export const NAV_ITEMS: NavItemConfig[] = [
  { view: 'dashboard', label: 'Dashboard', icon: '▦', group: 'Monitor' },
  { view: 'pipeline', label: 'Pipeline', icon: '▤', group: 'Build' },
  { view: 'schedules', label: 'Schedules', icon: '⏱', group: 'Build' },
  { view: 'workflows', label: 'Workflows', icon: '⤳', group: 'Insights' },
  { view: 'cost', label: 'Cost', icon: '◷', group: 'Insights' },
  { view: 'eval', label: 'Eval', icon: '⬡', group: 'Insights' },
]

export function viewTitle(view: ActiveView): string {
  return NAV_ITEMS.find(i => i.view === view)?.label ?? 'Dashboard'
}
