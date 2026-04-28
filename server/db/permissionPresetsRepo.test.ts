import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import process from 'node:process'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { closeDb, getDb } from './client.js'
import {
  deletePresetsForProject,
  listPresetProjectSummaries,
  listPresets,
  upsertPreset,
} from './permissionPresetsRepo.js'

let tmpDir: string

beforeEach(() => {
  tmpDir = mkdtempSync(join(tmpdir(), 'dashboard-presets-test-'))
  process.env.DASHBOARD_DB_PATH = join(tmpDir, 'test.db')
  getDb()
})

afterEach(() => {
  closeDb()
  rmSync(tmpDir, { recursive: true, force: true })
  delete process.env.DASHBOARD_DB_PATH
})

describe('upsertPreset + listPresets', () => {
  it('round-trips a preset entry', () => {
    upsertPreset('user-1', '/proj/a', 'Bash', 'ls *')
    const rows = listPresets('user-1', '/proj/a')
    expect(rows).toEqual([{ tool: 'Bash', pattern: 'ls *' }])
  })

  it('is idempotent on duplicate (user_id, cwd, tool, pattern)', () => {
    upsertPreset('user-1', '/proj/a', 'Bash', 'ls *')
    upsertPreset('user-1', '/proj/a', 'Bash', 'ls *')
    const rows = listPresets('user-1', '/proj/a')
    expect(rows).toHaveLength(1)
  })

  it('listPresets includes project-wide (user_id NULL) entries', () => {
    upsertPreset(null, '/proj/a', 'Read', null)
    upsertPreset('user-1', '/proj/a', 'Bash', 'ls *')

    const rows = listPresets('user-1', '/proj/a')
    expect(rows).toHaveLength(2)
    expect(rows).toContainEqual({ tool: 'Read', pattern: null })
    expect(rows).toContainEqual({ tool: 'Bash', pattern: 'ls *' })
  })
})

describe('listPresetProjectSummaries', () => {
  it('returns cwd + count grouped per project', () => {
    upsertPreset('user-1', '/proj/a', 'Bash', 'ls *')
    upsertPreset('user-1', '/proj/a', 'Read', null)
    upsertPreset('user-1', '/proj/b', 'Edit', null)

    const summaries = listPresetProjectSummaries('user-1')
    expect(summaries).toEqual([
      { cwd: '/proj/a', count: 2 },
      { cwd: '/proj/b', count: 1 },
    ])
  })

  it('includes project-wide (user_id NULL) entries in the count for a user', () => {
    upsertPreset(null, '/proj/a', 'Read', null)
    upsertPreset('user-1', '/proj/a', 'Bash', 'ls *')
    upsertPreset('user-2', '/proj/a', 'Edit', null)

    const summaries = listPresetProjectSummaries('user-1')
    // user-1 sees: own row + null-user row (NOT user-2's row)
    expect(summaries).toEqual([{ cwd: '/proj/a', count: 2 }])
  })

  it('returns empty array when no presets match', () => {
    upsertPreset('user-1', '/proj/a', 'Bash', null)
    expect(listPresetProjectSummaries('user-2')).toEqual([])
  })

  it('orders results by cwd', () => {
    upsertPreset('user-1', '/proj/z', 'Bash', null)
    upsertPreset('user-1', '/proj/a', 'Bash', null)
    upsertPreset('user-1', '/proj/m', 'Bash', null)

    const summaries = listPresetProjectSummaries('user-1')
    expect(summaries.map(s => s.cwd)).toEqual(['/proj/a', '/proj/m', '/proj/z'])
  })
})

describe('deletePresetsForProject', () => {
  it('removes only the matching (userId, cwd) combination', () => {
    upsertPreset('user-1', '/proj/a', 'Bash', null)
    upsertPreset('user-1', '/proj/a', 'Read', null)
    upsertPreset('user-1', '/proj/b', 'Edit', null)
    upsertPreset('user-2', '/proj/a', 'Bash', null)

    deletePresetsForProject('user-1', '/proj/a')

    // user-1's /proj/a entries gone
    expect(listPresets('user-1', '/proj/a')).toEqual([])
    // user-1's other project untouched
    expect(listPresets('user-1', '/proj/b')).toEqual([{ tool: 'Edit', pattern: null }])
    // user-2's entry in /proj/a untouched
    expect(listPresets('user-2', '/proj/a')).toEqual([{ tool: 'Bash', pattern: null }])
  })

  it('with userId=null removes only null-user entries (does NOT cascade to per-user rows)', () => {
    upsertPreset(null, '/proj/a', 'Read', null)
    upsertPreset(null, '/proj/a', 'Bash', null)
    upsertPreset('user-1', '/proj/a', 'Edit', null)

    deletePresetsForProject(null, '/proj/a')

    // null-user entries gone
    const db = getDb()
    const nullRows = db
      .prepare('SELECT COUNT(*) as c FROM permission_presets WHERE user_id IS NULL AND project_cwd = ?')
      .get('/proj/a') as { c: number }
    expect(nullRows.c).toBe(0)

    // user-1's entry still present
    const userRows = db
      .prepare('SELECT COUNT(*) as c FROM permission_presets WHERE user_id = ? AND project_cwd = ?')
      .get('user-1', '/proj/a') as { c: number }
    expect(userRows.c).toBe(1)
  })

  it('is a no-op when no entries match', () => {
    upsertPreset('user-1', '/proj/a', 'Bash', null)
    expect(() => deletePresetsForProject('user-1', '/proj/nonexistent')).not.toThrow()
    expect(listPresets('user-1', '/proj/a')).toHaveLength(1)
  })
})
