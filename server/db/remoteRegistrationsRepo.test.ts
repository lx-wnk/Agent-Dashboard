import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import process from 'node:process'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { closeDb, getDb } from './client.js'
import {
  createRemoteRegistration,
  deleteRemoteRegistration,
  listRemoteRegistrationsForUser,
} from './remoteRegistrationsRepo.js'
import { upsertUser } from './usersRepo.js'

let tmpDir: string

function seedUser(id = 'u1') {
  upsertUser({ id, githubLogin: 'alex', displayName: null, avatarUrl: null })
}

describe('remoteRegistrationsRepo', () => {
  beforeEach(() => {
    tmpDir = mkdtempSync(join(tmpdir(), 'dashboard-remotes-test-'))
    process.env.DASHBOARD_DB_PATH = join(tmpDir, 'test.db')
    getDb()
  })

  afterEach(() => {
    closeDb()
    rmSync(tmpDir, { recursive: true, force: true })
    delete process.env.DASHBOARD_DB_PATH
  })

  it('creates a registration and lists it for the owner', () => {
    seedUser()
    const reg = createRemoteRegistration({ userId: 'u1', url: 'http://192.168.1.5:13120', name: 'MacBook', bearerKey: 'tok' })
    const list = listRemoteRegistrationsForUser('u1')
    expect(list).toHaveLength(1)
    expect(list[0].id).toBe(reg.id)
    expect(list[0].url).toBe('http://192.168.1.5:13120')
  })

  it('does not return registrations for another user', () => {
    seedUser('u1')
    seedUser('u2')
    createRemoteRegistration({ userId: 'u1', url: 'http://a:13120', name: 'A', bearerKey: null })
    expect(listRemoteRegistrationsForUser('u2')).toHaveLength(0)
  })

  it('deletes a registration only when userId matches', () => {
    seedUser('u1')
    seedUser('u2')
    const reg = createRemoteRegistration({ userId: 'u1', url: 'http://a:13120', name: 'A', bearerKey: null })
    expect(deleteRemoteRegistration(reg.id, 'u2')).toBe(false)
    expect(deleteRemoteRegistration(reg.id, 'u1')).toBe(true)
    expect(listRemoteRegistrationsForUser('u1')).toHaveLength(0)
  })
})
