// Single source of truth for metric keys, the MetricKey union, and labels.
// Mirrors server/internal/eval/metrics.go (AllMetrics) — keep both in sync.
export const METRIC_KEYS = [
  'success_rate',
  'mean_iterations_to_success',
  'first_iter_validation_fail_rate',
  'awaiting_user_rate',
  'escalation_rate',
  'mean_duration_seconds',
  'mean_cost_cents',
  'mean_tokens',
  'timeout_rate',
] as const

export type MetricKey = typeof METRIC_KEYS[number]

export const METRIC_LABELS: Record<MetricKey, string> = {
  success_rate: 'Success rate',
  mean_iterations_to_success: 'Mean iterations to success',
  first_iter_validation_fail_rate: 'First-iter validation fail rate',
  awaiting_user_rate: 'Awaiting user rate',
  escalation_rate: 'Escalation rate',
  mean_duration_seconds: 'Mean duration (s)',
  mean_cost_cents: 'Mean cost (¢)',
  mean_tokens: 'Mean tokens',
  timeout_rate: 'Timeout rate',
}

export function metricLabel(key: string): string {
  return METRIC_LABELS[key as MetricKey] ?? key
}
