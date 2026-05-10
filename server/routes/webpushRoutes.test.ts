import type express from 'express'
import type { AddressInfo } from 'node:net'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import process from 'node:process'
import expressLib from 'express'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { closeDb, getDb } from '../db/client.js'
import { createWebPushRouter } from './webpushRoutes.js'

let tmpDir: string
let server: ReturnType<express.Express['listen']>
let baseUrl: string

beforeEach(async () => {
  tmpDir = mkdtempSync(join(tmpdir(), 'webpush-routes-test-'))
  process.env.DASHBOARD_DB_PATH = join(tmpDir, 'test.db')
  getDb()

  const app = expressLib()
  app.use(expressLib.json())
  app.use('/api', createWebPushRouter({ rejectCrossOrigin: () => false }))

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

describe('webpush routes', () => {
  it('returns 404 when no VAPID keys generated yet', async () => {
    const { status, data } = await api<{ error: string }>('GET', '/settings/webpush/vapid')
    expect(status).toBe(404)
    expect(data.error).toBe('VAPID keys not yet generated')
  })

  it('generates VAPID keys idempotently via POST', async () => {
    const r1 = await api<{ publicKey: string, alreadyGenerated?: boolean }>('POST', '/settings/webpush/vapid', {})
    expect(r1.status).toBe(200)
    expect(typeof r1.data.publicKey).toBe('string')
    expect(r1.data.alreadyGenerated).toBeUndefined()

    const r2 = await api<{ publicKey: string, alreadyGenerated: boolean }>('POST', '/settings/webpush/vapid', {})
    expect(r2.status).toBe(200)
    expect(r2.data.alreadyGenerated).toBe(true)
    expect(r2.data.publicKey).toBe(r1.data.publicKey)
  })

  it('returns public key via GET after generation', async () => {
    const r1 = await api<{ publicKey: string }>('POST', '/settings/webpush/vapid', {})
    expect(r1.status).toBe(200)

    const r2 = await api<{ publicKey: string }>('GET', '/settings/webpush/vapid')
    expect(r2.status).toBe(200)
    expect(r2.data.publicKey).toBe(r1.data.publicKey)
  })

  it('rejects subscribe with missing keys', async () => {
    const res = await api<{ error: string }>('POST', '/settings/webpush/subscribe', { endpoint: 'https://example.com' })
    expect(res.status).toBe(400)
    expect(res.data.error).toBe('Invalid subscription object')
  })

  it('rejects subscribe with missing endpoint', async () => {
    const res = await api<{ error: string }>('POST', '/settings/webpush/subscribe', {
      keys: { p256dh: 'abc', auth: 'def' },
    })
    expect(res.status).toBe(400)
  })

  it('accepts a valid push subscription', async () => {
    const res = await api<{ ok: boolean }>('POST', '/settings/webpush/subscribe', {
      endpoint: 'https://push.example.com/subscription/123',
      keys: { p256dh: 'BNbxTJDHalEqMaujw-mock-key', auth: 'mock-auth' },
    })
    expect(res.status).toBe(200)
    expect(res.data.ok).toBe(true)
  })
})
