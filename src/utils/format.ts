import type { ErrorState } from '../sdk.generated'
import type { TokenUsage } from '../types'

const MODEL_TRAILING_VERSION_RE = /-\d+$/

const ERROR_STATE_LABELS: Record<ErrorState, string> = {
  quota_exhausted: 'Quota exhausted',
  rate_limited: 'Rate limited',
  auth_failed: 'Authentication failed',
}

export function formatErrorState(state: ErrorState): string {
  return ERROR_STATE_LABELS[state] ?? 'Run failed'
}

export const STALLED_THRESHOLD_SECONDS = 180

export function secondsSince(iso: string | null, nowMs: number = Date.now()): number | null {
  if (!iso)
    return null
  const t = Date.parse(iso)
  if (Number.isNaN(t))
    return null
  return Math.max(0, Math.floor((nowMs - t) / 1000))
}

export function formatRelativeActivity(seconds: number | null): string {
  if (seconds == null)
    return '—'
  if (seconds < 60)
    return `${seconds}s ago`
  const m = Math.floor(seconds / 60)
  if (m < 60)
    return `${m}m ago`
  const h = Math.floor(m / 60)
  return `${h}h ${m % 60}m ago`
}

export function formatBurnRate(costUsd: number, uptimeSeconds: number): string {
  if (costUsd === 0 || uptimeSeconds === 0)
    return '—'
  const rate = costUsd / Math.max(1, uptimeSeconds / 60)
  return `$${rate.toFixed(2)}/min`
}

export function isStalled(status: string, secondsSinceActivity: number | null): boolean {
  return status === 'active' && secondsSinceActivity != null && secondsSinceActivity > STALLED_THRESHOLD_SECONDS
}

export function totalTokenCount(usage: TokenUsage): number {
  return usage.inputTokens + usage.outputTokens + usage.cacheReadTokens + usage.cacheCreationTokens
}

export function formatTokens(n: number): string {
  if (n === 0)
    return '—'
  if (n < 1000)
    return String(n)
  if (n < 1_000_000)
    return `${(n / 1000).toFixed(1)}k`
  return `${(n / 1_000_000).toFixed(2)}M`
}

export function formatCost(cost: number): string {
  if (cost === 0)
    return '—'
  if (cost < 0.01)
    return '<$0.01'
  return `$${cost.toFixed(2)}`
}

export function formatUptime(seconds: number): string {
  if (seconds < 60)
    return `${seconds}s`
  if (seconds < 3600)
    return `${Math.floor(seconds / 60)}m`
  if (seconds < 86400)
    return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`
  return `${Math.floor(seconds / 86400)}d ${Math.floor((seconds % 86400) / 3600)}h`
}

export function shortModel(model: string | null): string {
  if (!model)
    return '—'
  return model.replace('claude-', '').replace(MODEL_TRAILING_VERSION_RE, m => ` ${m.slice(1)}`)
}

export function maskToken(token: string): string {
  const head = token.slice(0, 8)
  if (token.length <= 12)
    return head + '•'.repeat(8)
  const tail = token.slice(-4)
  return head + '•'.repeat(token.length - 12) + tail
}
