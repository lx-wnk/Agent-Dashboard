import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import process from 'node:process'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { closeDb, getDb } from './client.js'
import { createTask, listTasksForUser } from './tasksRepo.js'
import { upsertUser } from './usersRepo.js'

let tmpDir: string

describe('tasksRepo — user_id + listTasksForUser', () => {
  beforeEach(() => {
    tmpDir = mkdtempSync(join(tmpdir(), 'tasksrepo-scoping-test-'))
    process.env.DASHBOARD_DB_PATH = join(tmpDir, 'test.db')
    getDb() // initialise schema
  })

  afterEach(() => {
    closeDb()
    rmSync(tmpDir, { recursive: true, force: true })
    delete process.env.DASHBOARD_DB_PATH
  })

  it('createTask persists user_id when provided', () => {
    upsertUser({ id: 'user-1', githubLogin: 'alice', displayName: null, avatarUrl: null })
    createTask({ slug: 'with-user', title: 'With User', cwd: '/x', userId: 'user-1' })

    const db = getDb()
    const row = db.prepare('SELECT user_id FROM tasks WHERE slug = ?').get('with-user') as { user_id: string | null }
    expect(row.user_id).toBe('user-1')
  })

  it('createTask sets user_id to NULL when omitted', () => {
    createTask({ slug: 'no-user', title: 'No User', cwd: '/x' })

    const db = getDb()
    const row = db.prepare('SELECT user_id FROM tasks WHERE slug = ?').get('no-user') as { user_id: string | null }
    expect(row.user_id).toBeNull()
  })

  it('createTask sets user_id to NULL when userId is explicitly null', () => {
    createTask({ slug: 'null-user', title: 'Null User', cwd: '/x', userId: null })

    const db = getDb()
    const row = db.prepare('SELECT user_id FROM tasks WHERE slug = ?').get('null-user') as { user_id: string | null }
    expect(row.user_id).toBeNull()
  })

  describe('listTasksForUser', () => {
    it('admin sees all tasks (own, others, and user_id=NULL)', () => {
      upsertUser({ id: 'admin', githubLogin: 'admin', displayName: null, avatarUrl: null })
      upsertUser({ id: 'user-1', githubLogin: 'alice', displayName: null, avatarUrl: null })
      upsertUser({ id: 'user-2', githubLogin: 'bob', displayName: null, avatarUrl: null })

      createTask({ slug: 'admin-task', title: 'Admin Task', cwd: '/a', userId: 'admin' })
      createTask({ slug: 'alice-task', title: 'Alice Task', cwd: '/a', userId: 'user-1' })
      createTask({ slug: 'bob-task', title: 'Bob Task', cwd: '/b', userId: 'user-2' })
      createTask({ slug: 'orphan-task', title: 'Orphan Task', cwd: '/o' })

      const result = listTasksForUser('admin', true)
      const slugs = result.map(t => t.slug).sort()
      expect(slugs).toEqual(['admin-task', 'alice-task', 'bob-task', 'orphan-task'])
    })

    it('regular user sees only their own tasks', () => {
      upsertUser({ id: 'user-1', githubLogin: 'alice', displayName: null, avatarUrl: null })
      upsertUser({ id: 'user-2', githubLogin: 'bob', displayName: null, avatarUrl: null })

      createTask({ slug: 'alice-1', title: 'A1', cwd: '/a', userId: 'user-1' })
      createTask({ slug: 'alice-2', title: 'A2', cwd: '/a', userId: 'user-1' })
      createTask({ slug: 'bob-1', title: 'B1', cwd: '/b', userId: 'user-2' })

      const result = listTasksForUser('user-1', false)
      const slugs = result.map(t => t.slug).sort()
      expect(slugs).toEqual(['alice-1', 'alice-2'])
    })

    it('regular user does NOT see tasks with user_id=NULL', () => {
      upsertUser({ id: 'user-1', githubLogin: 'alice', displayName: null, avatarUrl: null })

      createTask({ slug: 'alice-1', title: 'A1', cwd: '/a', userId: 'user-1' })
      createTask({ slug: 'orphan', title: 'Orphan', cwd: '/o' }) // no userId
      createTask({ slug: 'orphan2', title: 'Orphan2', cwd: '/o', userId: null })

      const result = listTasksForUser('user-1', false)
      const slugs = result.map(t => t.slug).sort()
      expect(slugs).toEqual(['alice-1'])
    })

    it('regular user does NOT see another user\'s tasks', () => {
      upsertUser({ id: 'user-1', githubLogin: 'alice', displayName: null, avatarUrl: null })
      upsertUser({ id: 'user-2', githubLogin: 'bob', displayName: null, avatarUrl: null })

      createTask({ slug: 'bob-1', title: 'B1', cwd: '/b', userId: 'user-2' })

      const result = listTasksForUser('user-1', false)
      expect(result).toHaveLength(0)
    })

    it('returns tasks ordered by created_at DESC', () => {
      upsertUser({ id: 'user-1', githubLogin: 'alice', displayName: null, avatarUrl: null })

      // ISO timestamps have ms granularity, so back-to-back inserts can tie.
      // Manually backdate the rows to guarantee a strict ordering.
      createTask({ slug: 'first', title: 'First', cwd: '/x', userId: 'user-1' })
      createTask({ slug: 'second', title: 'Second', cwd: '/x', userId: 'user-1' })
      createTask({ slug: 'third', title: 'Third', cwd: '/x', userId: 'user-1' })

      const db = getDb()
      db.prepare('UPDATE tasks SET created_at = ? WHERE slug = ?').run('2026-01-01T00:00:00.000Z', 'first')
      db.prepare('UPDATE tasks SET created_at = ? WHERE slug = ?').run('2026-01-02T00:00:00.000Z', 'second')
      db.prepare('UPDATE tasks SET created_at = ? WHERE slug = ?').run('2026-01-03T00:00:00.000Z', 'third')

      const result = listTasksForUser('user-1', false)
      expect(result.length).toBe(3)
      expect(result[0].slug).toBe('third')
      expect(result[1].slug).toBe('second')
      expect(result[2].slug).toBe('first')
    })

    it('admin with no tasks returns empty list (no rows)', () => {
      const result = listTasksForUser('admin', true)
      expect(result).toEqual([])
    })

    it('regular user with no matching tasks returns empty list', () => {
      upsertUser({ id: 'user-1', githubLogin: 'alice', displayName: null, avatarUrl: null })
      createTask({ slug: 'orphan', title: 'O', cwd: '/o' })

      const result = listTasksForUser('user-1', false)
      expect(result).toEqual([])
    })

    it('returned tasks include enriched isBlocked / isUnsatisfiable fields', () => {
      upsertUser({ id: 'user-1', githubLogin: 'alice', displayName: null, avatarUrl: null })
      createTask({ slug: 'solo', title: 'Solo', cwd: '/s', userId: 'user-1' })

      const result = listTasksForUser('user-1', false)
      expect(result[0].isBlocked).toBe(false)
      expect(result[0].isUnsatisfiable).toBe(false)
    })
  })
})
