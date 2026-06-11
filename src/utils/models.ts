export const AVAILABLE_MODELS = [
  'claude-opus-4-8',
  'claude-opus-4-6',
  'claude-sonnet-4-6',
  'claude-haiku-4-5',
] as const

export type AvailableModel = typeof AVAILABLE_MODELS[number]
