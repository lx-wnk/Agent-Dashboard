import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import process from 'node:process'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { closeDb, getDb } from './client.js'
import { deleteTurnsForTask, insertTurn, listTurns } from './refinementTurnsRepo.js'

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

describe('insertTurn', () => {
  it('persists a turn and returns it', () => {
    insertTask('t1')
    const turn = insertTurn({ taskId: 't1', role: 'user', content: 'hello' })
    expect(turn.id).toBeTruthy()
    expect(turn.role).toBe('user')
    expect(turn.content).toBe('hello')
    expect(turn.phase).toBeNull()
  })

  it('persists the phase when provided', () => {
    insertTask('t1b')
    const turn = insertTurn({ taskId: 't1b', role: 'assistant', content: 'hi', phase: 'analysis' })
    expect(turn.phase).toBe('analysis')
  })
})

describe('listTurns', () => {
  it('returns turns in insertion order', () => {
    insertTask('t2')
    insertTurn({ taskId: 't2', role: 'user', content: 'A' })
    insertTurn({ taskId: 't2', role: 'assistant', content: 'B', phase: 'analysis' })
    const turns = listTurns('t2')
    expect(turns).toHaveLength(2)
    expect(turns[0].role).toBe('user')
    expect(turns[1].phase).toBe('analysis')
  })

  it('returns empty array for unknown task', () => {
    expect(listTurns('unknown')).toEqual([])
  })
})

describe('deleteTurnsForTask', () => {
  it('deletes all turns for the given task', () => {
    insertTask('t3')
    insertTurn({ taskId: 't3', role: 'user', content: 'A' })
    insertTurn({ taskId: 't3', role: 'assistant', content: 'B' })
    expect(listTurns('t3')).toHaveLength(2)
    deleteTurnsForTask('t3')
    expect(listTurns('t3')).toEqual([])
  })

  it('does not delete turns for other tasks', () => {
    insertTask('t4')
    insertTask('t5')
    insertTurn({ taskId: 't4', role: 'user', content: 'keep' })
    insertTurn({ taskId: 't5', role: 'user', content: 'delete' })
    deleteTurnsForTask('t5')
    expect(listTurns('t4')).toHaveLength(1)
    expect(listTurns('t5')).toEqual([])
  })

  it('is a no-op for unknown task', () => {
    expect(() => deleteTurnsForTask('unknown')).not.toThrow()
  })
})
