import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import process from 'node:process'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { closeDb, getDb } from '../db/client.js'
import { createStageRun, updateStageRun } from '../db/stageRunsRepo.js'
import { createTask } from '../db/tasksRepo.js'
import { attachSessionId, buildSessionName, decideRecovery, isPidAlive } from './sessionManager.js'

let tmpDir: string

beforeEach(() => {
  tmpDir = mkdtempSync(join(tmpdir(), 'pipeline-sess-test-'))
  process.env.DASHBOARD_DB_PATH = join(tmpDir, 'test.db')
  getDb()
})

afterEach(() => {
  closeDb()
  rmSync(tmpDir, { recursive: true, force: true })
  delete process.env.DASHBOARD_DB_PATH
})

describe('buildSessionName', () => {
  it('formats slug-stage-iter-n', () => {
    const task = createTask({ slug: 'fix-login-bug', title: 'X', cwd: '/x' })
    expect(buildSessionName(task, 'umsetzung', 3)).toBe('fix-login-bug-umsetzung-iter-3')
  })
})

describe('isPidAlive', () => {
  it('returns true for current process pid', () => {
    expect(isPidAlive(process.pid)).toBe(true)
  })

  it('returns false for null / non-positive pid', () => {
    expect(isPidAlive(null)).toBe(false)
    expect(isPidAlive(0)).toBe(false)
    expect(isPidAlive(-1)).toBe(false)
  })

  it('returns false for a surely-dead pid', () => {
    // 2^31-1 is well beyond any realistic running pid.
    expect(isPidAlive(2147483647)).toBe(false)
  })
})

describe('decideRecovery', () => {
  it('returns alive for live pid', () => {
    const task = createTask({ slug: 'a', title: 'A', cwd: '/a' })
    const run = createStageRun({ taskId: task.id, stage: 'umsetzung' })
    updateStageRun(run.id, { pid: process.pid, status: 'running' })
    const updated = { ...run, pid: process.pid, status: 'running' as const }
    expect(decideRecovery(updated).kind).toBe('alive')
  })

  it('returns resume when pid is dead but session_id exists', () => {
    const task = createTask({ slug: 'b', title: 'B', cwd: '/b' })
    const run = createStageRun({ taskId: task.id, stage: 'umsetzung' })
    updateStageRun(run.id, { pid: 2147483647, sessionId: 'sess-1', status: 'running' })
    const updated = {
      ...run,
      pid: 2147483647,
      sessionId: 'sess-1',
      status: 'running' as const,
    }
    expect(decideRecovery(updated).kind).toBe('resume')
  })

  it('returns restart when neither pid nor session exist', () => {
    const task = createTask({ slug: 'c', title: 'C', cwd: '/c' })
    const run = createStageRun({ taskId: task.id, stage: 'umsetzung' })
    expect(decideRecovery(run).kind).toBe('restart')
  })
})

describe('attachSessionId', () => {
  it('persists a session id to a stage run', () => {
    const task = createTask({ slug: 'd', title: 'D', cwd: '/d' })
    const run = createStageRun({ taskId: task.id, stage: 'umsetzung' })
    attachSessionId(run.id, 'uuid-xyz')
    // Re-read via a direct query to verify.
    const db = getDb()
    const row = db.prepare('SELECT session_id FROM stage_runs WHERE id = ?').get(run.id) as { session_id: string }
    expect(row.session_id).toBe('uuid-xyz')
  })
})
