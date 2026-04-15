import type express from 'express'
import type { AddressInfo } from 'node:net'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import process from 'node:process'
import expressLib from 'express'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { closeDb, getDb } from '../db/client.js'
import { createApiKeyRouter } from './apiKeyRoutes.js'

let tmpDir: string
let server: ReturnType<express.Express['listen']>
let baseUrl: string

beforeEach(async () => {
  tmpDir = mkdtempSync(join(tmpdir(), 'api-key-routes-test-'))
  process.env.DASHBOARD_DB_PATH = join(tmpDir, 'test.db')
  getDb()

  const app = expressLib()
  app.use(expressLib.json())
  app.use('/api', createApiKeyRouter({
    rejectCrossOrigin: () => false, // allow everything in tests
  }))

  server = await new Promise<ReturnType<express.Express['listen']>>((resolve) => {
    const s = app.listen(0, '127.0.0.1', () => resolve(s))
  })
  const addr = server.address() as AddressInfo
  baseUrl = `http://127.0.0.1:${addr.port}/api`
})

afterEach(() => {
  server?.close()
  closeDb()
  rmSync(tmpDir, { recursive: true, force: true })
  delete process.env.DASHBOARD_DB_PATH
})

async function api<T = unknown>(method: string, path: string, body?: unknown): Promise<{ status: number, data: T }> {
  const res = await fetch(baseUrl + path, {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  const text = await res.text()
  const data = text ? JSON.parse(text) as T : ({} as T)
  return { status: res.status, data }
}

describe('gET /api/settings/api-keys', () => {
  it('returns 200 with an array (empty when no keys exist)', async () => {
    const { status, data } = await api<unknown[]>('GET', '/settings/api-keys')
    expect(status).toBe(200)
    expect(Array.isArray(data)).toBe(true)
  })
})

describe('pOST /api/settings/api-keys', () => {
  it('creates a key and returns 201 with key + token', async () => {
    const { status, data } = await api<{ key: { id: string, name: string }, token: string }>(
      'POST',
      '/settings/api-keys',
      { name: 'My Key', scopes: ['tasks:read'] },
    )
    expect(status).toBe(201)
    expect(typeof data.token).toBe('string')
    expect(data.token).toMatch(/^mcp_/)
    expect(data.key.name).toBe('My Key')
    expect(typeof data.key.id).toBe('string')
  })

  it('returns 400 when name is missing', async () => {
    const { status, data } = await api<{ error: string }>(
      'POST',
      '/settings/api-keys',
      { scopes: ['tasks:read'] },
    )
    expect(status).toBe(400)
    expect(data.error).toMatch(/name/i)
  })

  it('returns 400 when scopes is empty', async () => {
    const { status, data } = await api<{ error: string }>(
      'POST',
      '/settings/api-keys',
      { name: 'Bad Key', scopes: [] },
    )
    expect(status).toBe(400)
    expect(data.error).toMatch(/scopes/i)
  })

  it('returns 400 for an invalid scope value', async () => {
    const { status, data } = await api<{ error: string }>(
      'POST',
      '/settings/api-keys',
      { name: 'Bad Scope', scopes: ['not:a:real:scope'] },
    )
    expect(status).toBe(400)
    expect(data.error).toMatch(/invalid scope/i)
  })

  it('returns 409 when name already exists', async () => {
    await api('POST', '/settings/api-keys', { name: 'Dup', scopes: ['tasks:read'] })
    const { status, data } = await api<{ error: string }>(
      'POST',
      '/settings/api-keys',
      { name: 'Dup', scopes: ['tasks:write'] },
    )
    expect(status).toBe(409)
    expect(data.error).toMatch(/already exists/i)
  })
})

describe('dELETE /api/settings/api-keys/:id', () => {
  it('revokes an existing key and returns 204', async () => {
    // Create a key first
    const { data: created } = await api<{ key: { id: string } }>(
      'POST',
      '/settings/api-keys',
      { name: 'To Revoke', scopes: ['tasks:write'] },
    )
    const { status } = await api('DELETE', `/settings/api-keys/${created.key.id}`)
    expect(status).toBe(204)

    // The key should no longer appear in the active list
    const { data: list } = await api<Array<{ id: string }>>('GET', '/settings/api-keys')
    expect(list.some(k => k.id === created.key.id)).toBe(false)
  })

  it('returns 404 for a nonexistent key id', async () => {
    const { status, data } = await api<{ error: string }>(
      'DELETE',
      '/settings/api-keys/00000000-0000-0000-0000-000000000000',
    )
    expect(status).toBe(404)
    expect(data.error).toMatch(/not found/i)
  })
})
