// Pricing per 1M tokens (USD) — Claude Code Pro/Max users pay via subscription,
// but we estimate API-equivalent cost for visibility
export const MODEL_PRICING: Record<string, { input: number, output: number, cacheRead: number, cacheCreate: number }> = {
  'claude-opus-4-6': { input: 15, output: 75, cacheRead: 1.5, cacheCreate: 18.75 },
  'claude-opus-4-0': { input: 15, output: 75, cacheRead: 1.5, cacheCreate: 18.75 },
  'claude-sonnet-4-6': { input: 3, output: 15, cacheRead: 0.3, cacheCreate: 3.75 },
  'claude-sonnet-4-5': { input: 3, output: 15, cacheRead: 0.3, cacheCreate: 3.75 },
  'claude-haiku-4-5': { input: 0.8, output: 4, cacheRead: 0.08, cacheCreate: 1 },
}

const DEFAULT_MODEL = 'claude-sonnet-4-6'

export interface TokenCounts {
  inputTokens: number
  outputTokens: number
  cacheReadTokens?: number
  cacheCreationTokens?: number
}

export function estimateCost(usage: TokenCounts, model: string | null): number {
  const pricing = (model && MODEL_PRICING[model]) || MODEL_PRICING[DEFAULT_MODEL]
  const m = 1_000_000
  return (
    (usage.inputTokens * pricing.input) / m
    + (usage.outputTokens * pricing.output) / m
    + ((usage.cacheReadTokens || 0) * pricing.cacheRead) / m
    + ((usage.cacheCreationTokens || 0) * pricing.cacheCreate) / m
  )
}
