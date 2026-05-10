import type { Database } from './client.js'

export interface CostRow {
  t: number
  cost: number
  tokens: number
}

export function bulkInsertHistoricalCost(db: Database, rows: CostRow[]): void {
  const insert = db.prepare('INSERT OR IGNORE INTO agent_cost_trend (t, cost, tokens) VALUES (?, ?, ?)')
  const insertAll = db.transaction((r: CostRow[]) => {
    for (const row of r)
      insert.run(row.t, row.cost, row.tokens)
  })
  insertAll(rows)
}
