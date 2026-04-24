import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import process from 'node:process'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { closeDb, getDb } from './client.js'
import { createTurn, listTurns } from './refinementTurnsRepo.js'

let tmpDir: string

beforeEach(() => {
  tmpDir = mkdtempSync(join(tmpdir(), 'dashboard-refinement-test-'))
  process.env.DASHBOARD_DB_PATH = join(tmpDir, 'test.db')
  getDb()
})

afterEach(() => {
  closeDb()
  rmSync(tmpDir, { recursive: true, force: true })
  delete process.env.DASHBOARD_DB_PATH
})

function insertTask(id: string) {
  getDb().prepare(`
    INSERT INTO tasks (id, slug, title, cwd, current_stage, max_iterations,
      stage_timeout_seconds, silver_bullet, priority, created_at, updated_at)
    VALUES (?, ?, 'T', '/tmp', 'refinement', 20, 1800, 0, 'medium', '2026-01-01', '2026-01-01')
  `).run(id, id)
}

describe('createTurn', () => {
  it('persists a turn and returns it', () => {
    insertTask('t1')
    const turn = createTurn({ taskId: 't1', role: 'user', content: 'hello', phase: null })
    expect(turn.id).toBeTruthy()
    expect(turn.role).toBe('user')
    expect(turn.content).toBe('hello')
  })
})

describe('listTurns', () => {
  it('returns turns in insertion order', () => {
    insertTask('t2')
    createTurn({ taskId: 't2', role: 'user', content: 'A', phase: null })
    createTurn({ taskId: 't2', role: 'assistant', content: 'B', phase: 'analyse' })
    const turns = listTurns('t2')
    expect(turns).toHaveLength(2)
    expect(turns[0].role).toBe('user')
    expect(turns[1].phase).toBe('analyse')
  })

  it('returns empty array for unknown task', () => {
    expect(listTurns('unknown')).toEqual([])
  })
})
