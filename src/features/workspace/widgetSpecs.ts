export interface WidgetSpec {
  id: string
  title: string
  defaultColSpan: number
  defaultRowSpan: number
  minColSpan: number
  minRowSpan: number
}

function spec(id: string, title: string, def: [number, number], min: [number, number]): WidgetSpec {
  return { id, title, defaultColSpan: def[0], defaultRowSpan: def[1], minColSpan: min[0], minRowSpan: min[1] }
}

// Spans from the Zentrale spec's widget table. Plain data on purpose: the layout
// rules import this without pulling in a single component.
export const WIDGET_SPECS: Record<string, WidgetSpec> = Object.fromEntries([
  spec('kontor', 'Kontor', [6, 1], [4, 1]),
  spec('live-work', 'Live work', [3, 5], [3, 3]),
  spec('agents', 'Agents', [3, 3], [3, 2]),
  spec('pipeline', 'Pipeline', [3, 3], [3, 2]),
  spec('routines', 'Routines', [3, 4], [3, 2]),
  spec('github', 'GitHub', [3, 3], [3, 2]),
  spec('memory', 'Memory', [3, 3], [3, 2]),
  spec('cost-today', 'Today', [3, 3], [2, 2]),
].map(s => [s.id, s]))
