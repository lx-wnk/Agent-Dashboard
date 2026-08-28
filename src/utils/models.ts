export const AVAILABLE_MODELS = [
  'claude-opus-4-8',
  'claude-opus-4-6',
  'claude-sonnet-4-6',
  'claude-haiku-4-5',
] as const

export type AvailableModel = typeof AVAILABLE_MODELS[number]

// Reasoning-effort levels for the claude adapter's adapter_config.effort key
// (server/internal/services/effort_resolver.go). The server stores this as an
// arbitrary string — no Go-side enum exists to derive from — so this list is
// hand-kept in parity with what the claude CLI actually accepts.
export const EFFORT_OPTIONS = [
  { value: 'low', label: 'Low' },
  { value: 'medium', label: 'Medium' },
  { value: 'high', label: 'High' },
] as const

export type EffortLevel = typeof EFFORT_OPTIONS[number]['value']
