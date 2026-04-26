import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import process from 'node:process'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { closeDb, getDb } from './client.js'
import { findUserById, upsertUser } from './usersRepo.js'

let tmpDir: string

describe('usersRepo', () => {
  beforeEach(() => {
    tmpDir = mkdtempSync(join(tmpdir(), 'dashboard-users-test-'))
    process.env.DASHBOARD_DB_PATH = join(tmpDir, 'test.db')
    getDb() // initialise schema
  })
  afterEach(() => {
    closeDb()
    rmSync(tmpDir, { recursive: true, force: true })
    delete process.env.DASHBOARD_DB_PATH
  })

  it('upserts a new user and returns it', () => {
    const user = upsertUser({ id: '12345', githubLogin: 'alex', displayName: 'Alex W', avatarUrl: 'https://gh.io/av' })
    expect(user.id).toBe('12345')
    expect(user.githubLogin).toBe('alex')
    expect(user.isAdmin).toBe(false)
    expect(user.createdAt).toBeTruthy()
  })

  it('updates githubLogin and lastLoginAt on subsequent upsert', () => {
    upsertUser({ id: '12345', githubLogin: 'alex', displayName: null, avatarUrl: null })
    const updated = upsertUser({ id: '12345', githubLogin: 'alex-new', displayName: null, avatarUrl: null })
    expect(updated.githubLogin).toBe('alex-new')
    expect(updated.lastLoginAt).toBeTruthy()
  })

  it('findUserById returns null for unknown id', () => {
    expect(findUserById('unknown')).toBeNull()
  })

  it('findUserById returns the user after upsert', () => {
    upsertUser({ id: '99', githubLogin: 'bob', displayName: null, avatarUrl: null })
    const found = findUserById('99')
    expect(found?.githubLogin).toBe('bob')
  })
})
