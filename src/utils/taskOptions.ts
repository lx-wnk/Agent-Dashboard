import type { TaskAutonomy, TaskPriority } from '../types'

// `satisfies` rather than deriving the unions from these arrays: autonomy and
// priority are wire fields, so the contract has to own the type and the dropdown
// has to prove it covers it. Deriving the other way round would let hiding an
// option silently narrow PipelineTask, with nothing failing to compile.
// (utils/agentGroup.ts derives its unions from the arrays on purpose — AgentSort
// and AgentGroup are client-only view state, never sent to or from the server.)
export const TASK_PRIORITY_OPTIONS = [
  { value: 'high', label: 'High' },
  { value: 'medium', label: 'Medium' },
  { value: 'low', label: 'Low' },
] as const satisfies readonly { value: TaskPriority, label: string }[]

export const TASK_AUTONOMY_OPTIONS = [
  { value: 'manual', label: 'Manual — approve every stage' },
  { value: 'spec_gated', label: 'Spec-gated — approve the spec, then autonomous' },
  { value: 'full', label: 'Full — fully autonomous' },
] as const satisfies readonly { value: TaskAutonomy, label: string }[]
