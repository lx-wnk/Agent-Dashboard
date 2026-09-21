import type { Component } from 'vue'
import type { WidgetSpec } from './widgetSpecs'
import { CostTodayWidget } from '@/features/analytics'
import { AgentsPanel, GitHubPanel, MemoryPanel, PipelinePanel, RoutinesPanel } from '@/features/cockpit'
import { KontorWidget, LiveWorkWidget } from '@/features/mission'
import { WIDGET_SPECS } from './widgetSpecs'

export type WidgetDef = WidgetSpec & { component: Component }

const COMPONENTS: Record<string, Component> = {
  'kontor': KontorWidget,
  'live-work': LiveWorkWidget,
  'agents': AgentsPanel,
  'pipeline': PipelinePanel,
  'routines': RoutinesPanel,
  'github': GitHubPanel,
  'memory': MemoryPanel,
  'cost-today': CostTodayWidget,
}

export const WIDGETS: Record<string, WidgetDef> = Object.fromEntries(
  Object.entries(COMPONENTS).map(([id, component]) => [id, { ...WIDGET_SPECS[id], component }]),
)

export function widgetIds(): string[] {
  return Object.keys(WIDGETS)
}
