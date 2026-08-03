export const TASK_PRIORITY_OPTIONS = [
  { value: 'high', label: 'High' },
  { value: 'medium', label: 'Medium' },
  { value: 'low', label: 'Low' },
] as const

export const TASK_AUTONOMY_OPTIONS = [
  { value: 'manual', label: 'Manual — approve every stage' },
  { value: 'spec_gated', label: 'Spec-gated — approve the spec, then autonomous' },
  { value: 'full', label: 'Full — fully autonomous' },
] as const

export type TaskPriority = typeof TASK_PRIORITY_OPTIONS[number]['value']
export type TaskAutonomy = typeof TASK_AUTONOMY_OPTIONS[number]['value']
