import type { TokenUsage } from '../types'

export function totalTokenCount(usage: TokenUsage): number {
  return usage.inputTokens + usage.outputTokens + usage.cacheReadTokens + usage.cacheCreationTokens
}

export function formatTokens(n: number): string {
  if (n === 0) return '—'
  if (n < 1000) return String(n)
  if (n < 1_000_000) return `${(n / 1000).toFixed(1)}k`
  return `${(n / 1_000_000).toFixed(2)}M`
}

export function formatCost(cost: number): string {
  if (cost === 0) return '—'
  if (cost < 0.01) return '<$0.01'
  return `$${cost.toFixed(2)}`
}

export function formatUptime(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`
  return `${Math.floor(seconds / 86400)}d ${Math.floor((seconds % 86400) / 3600)}h`
}

export function shortModel(model: string | null): string {
  if (!model) return '—'
  return model.replace('claude-', '').replace(/-\d+$/, m => ' ' + m.slice(1))
}
