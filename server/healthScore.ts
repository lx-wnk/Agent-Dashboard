export interface HealthScoreInput {
  completedTasks: number
  totalTasks: number
  cacheReadTokens: number
  inputTokens: number
  hasError: boolean
  costEstimate: number
  recentAvgCost: number
}

export function computeHealthScore(input: HealthScoreInput): number {
  const {
    completedTasks,
    totalTasks,
    cacheReadTokens,
    inputTokens,
    hasError,
    costEstimate,
    recentAvgCost,
  } = input

  const successRate = (completedTasks / Math.max(totalTasks, 1)) * 100
  const cacheHitRate = (cacheReadTokens / Math.max(inputTokens + cacheReadTokens, 1)) * 100
  const errorScore = hasError ? 0 : 100
  let costSpikeScore = 100
  if (recentAvgCost > 0 && costEstimate > recentAvgCost * 3)
    costSpikeScore = 0

  const score
    = successRate * 0.4
    + cacheHitRate * 0.25
    + errorScore * 0.25
    + costSpikeScore * 0.1

  return Math.round(Math.max(0, Math.min(100, score)))
}
