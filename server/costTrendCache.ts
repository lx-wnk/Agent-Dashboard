import { getDb } from './db/client.js'

/**
 * Returns the mean fleet cost across trend snapshots within the last
 * `windowMs` milliseconds. Each row in `agent_cost_trend` stores the
 * *total cumulative cost across all running agents* at that tick —
 * not a per-agent value. Returns 0 when fewer than 2 data points exist
 * (insufficient history). Callers that need a per-agent baseline should
 * divide the result by the agent count before comparing.
 */
export function getRecentAvgFleetCost(windowMs: number): number {
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
  catch (err) {
    const code = (err as NodeJS.ErrnoException).code
    if (code !== 'SQLITE_ERROR' && code !== 'ENOENT')
      console.warn('[costTrendCache] unexpected error:', err)
    return 0
  }
}
