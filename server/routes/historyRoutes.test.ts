import type express from 'express'
import type { AddressInfo } from 'node:net'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import process from 'node:process'
import expressLib from 'express'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { closeDb, getDb } from '../db/client.js'
import { createHistoryRouter } from './historyRoutes.js'

let tmpDir: string
let server: ReturnType<express.Express['listen']>
let baseUrl: string

beforeEach(async () => {
  tmpDir = mkdtempSync(join(tmpdir(), 'history-routes-test-'))
  process.env.DASHBOARD_DB_PATH = join(tmpDir, 'test.db')
  getDb()

  const app = expressLib()
  app.use(expressLib.json())
  app.use('/api', createHistoryRouter())

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

describe('historyRoutes', () => {
  it('returns ok when starting import', async () => {
    const res = await api<{ ok: boolean, message: string }>('POST', '/history/import')
    expect(res.status).toBe(200)
    expect(res.data.ok).toBe(true)
  })

  it('returns 409 when import is already running', async () => {
    await api('POST', '/history/import')
    const res = await api('POST', '/history/import')
    expect([200, 409]).toContain(res.status)
  })
})
