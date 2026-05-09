import type express from 'express'
import type { AddressInfo } from 'node:net'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import process from 'node:process'
import expressLib from 'express'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { closeDb, getDb } from '../db/client.js'
import { createTask } from '../db/tasksRepo.js'
import { createSearchRouter } from './searchRoutes.js'

let tmpDir: string
let server: ReturnType<express.Express['listen']>
let baseUrl: string

beforeEach(() => {
  tmpDir = mkdtempSync(join(tmpdir(), 'search-routes-test-'))
  process.env.DASHBOARD_DB_PATH = join(tmpDir, 'test.db')
  getDb()

  const app = expressLib()
  app.use(expressLib.json())
  // Inject a test user (admin) so req.user! in the search handler is populated.
  app.use((_req, _res, next) => {
    (_req as typeof _req & { user: { id: string, login: string, isAdmin: boolean } }).user
      = { id: 'test-user', login: 'test', isAdmin: true }
    next()
  })
  app.use('/api', createSearchRouter({ getAgents: () => [] }))
  server = app.listen(0)
  baseUrl = `http://localhost:${(server.address() as AddressInfo).port}`
})

afterEach(async () => {
  server.close()
  closeDb()
  rmSync(tmpDir, { recursive: true, force: true })
  delete process.env.DASHBOARD_DB_PATH
})

async function api(path: string): Promise<{ status: number, body: unknown }> {
  const res = await fetch(`${baseUrl}${path}`)
  return { status: res.status, body: await res.json() }
}

describe('searchRoutes', () => {
  it('returns empty results for blank query', async () => {
    const { status, body } = await api('/api/search?q=')
    expect(status).toBe(200)
    expect((body as { tasks: unknown[], agents: unknown[] }).tasks).toEqual([])
    expect((body as { tasks: unknown[], agents: unknown[] }).agents).toEqual([])
  })

  it('does not throw on FTS syntax error query', async () => {
    const { status } = await api('/api/search?q=AND')
    expect(status).toBe(200)
  })

  it('returns task matching title via FTS', async () => {
    createTask({
      slug: 'my-feature',
      title: 'Implement the login feature',
      description: 'OAuth2 integration',
      cwd: '/tmp/proj',
    })
    const { status, body } = await api('/api/search?q=login')
    expect(status).toBe(200)
    const tasks = (body as { tasks: Array<{ slug: string }>, agents: unknown[] }).tasks
    expect(tasks.some(t => t.slug === 'my-feature')).toBe(true)
  })

  it('filters by type=tasks and excludes agents', async () => {
    const { status, body } = await api('/api/search?q=test&type=tasks')
    expect(status).toBe(200)
    const result = body as { tasks: unknown[], agents: unknown[] }
    expect(Array.isArray(result.tasks)).toBe(true)
    expect(Array.isArray(result.agents)).toBe(true)
    expect(result.agents).toHaveLength(0)
  })

  it('respects limit parameter', async () => {
    for (let i = 0; i < 5; i++) {
      createTask({
        slug: `task-limit-${i}`,
        title: `Searchable task number ${i}`,
        cwd: '/tmp/proj',
      })
    }
    const { status, body } = await api('/api/search?q=Searchable&limit=2')
    expect(status).toBe(200)
    const tasks = (body as { tasks: unknown[], agents: unknown[] }).tasks
    expect(tasks.length).toBeLessThanOrEqual(2)
  })
})
