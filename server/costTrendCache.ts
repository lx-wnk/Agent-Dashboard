import { getDb } from './db/client.js'

/**
 * Returns the mean accumulated cost across trend points within the last
 * `windowMs` milliseconds. Returns 0 if fewer than 2 data points exist
 * (insufficient history). Used by the health score as a baseline for
 * cost-spike detection.
 */
export function getRecentAvgCostPerHour(windowMs: number): number {
  try {
    const db = getDb()
    const since = Date.now() - windowMs
    const rows = db
      .prepare('SELECT cost FROM agent_cost_trend WHERE t >= ? ORDER BY t ASC')
      .all(since) as Array<{ cost: number }>
    if (rows.length < 2)
      return 0
    const sum = rows.reduce((acc, r) => acc + r.cost, 0)
    return sum / rows.length
  }
  catch {
    return 0
  }
}
