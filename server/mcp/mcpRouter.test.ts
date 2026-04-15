import type express from 'express'
import type { AddressInfo } from 'node:net'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import process from 'node:process'
import expressLib from 'express'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { closeDb, getDb } from '../db/client.js'
import { createApiKey } from '../db/apiKeysRepo.js'
import { createHash, randomBytes } from 'node:crypto'
import { PipelineOrchestrator } from '../pipeline/orchestrator.js'
import { createMcpRouter } from './mcpRouter.js'

let tmpDir: string
let server: ReturnType<express.Express['listen']>
let baseUrl: string
let orchestrator: PipelineOrchestrator

beforeEach(async () => {
  tmpDir = mkdtempSync(join(tmpdir(), 'mcp-router-test-'))
  process.env.DASHBOARD_DB_PATH = join(tmpDir, 'test.db')
  getDb()

  orchestrator = new PipelineOrchestrator()
  const app = expressLib()
  app.use(expressLib.json())
  app.use('/api', createMcpRouter(orchestrator, () => {}))

  server = await new Promise<ReturnType<express.Express['listen']>>((resolve) => {
    const s = app.listen(0, '127.0.0.1', () => resolve(s))
  })
  const addr = server.address() as AddressInfo
  baseUrl = `http://127.0.0.1:${addr.port}/api`
})

afterEach(() => {
  orchestrator.stop()
  server?.close()
  closeDb()
  rmSync(tmpDir, { recursive: true, force: true })
  delete process.env.DASHBOARD_DB_PATH
})

describe('POST /api/mcp', () => {
  it('returns 401 when Authorization header is missing', async () => {
    const res = await fetch(`${baseUrl}/mcp`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({}),
    })
    expect(res.status).toBe(401)
  })

  it('returns 401 when bearer token is invalid', async () => {
    const res = await fetch(`${baseUrl}/mcp`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': 'Bearer invalid-token-xyz',
      },
      body: JSON.stringify({}),
    })
    expect(res.status).toBe(401)
  })

  it('returns something other than 401 for a valid bearer token', async () => {
    // Create a real API key so mcpAuthMiddleware accepts it
    const token = `mcp_${randomBytes(16).toString('hex')}`
    const keyHash = createHash('sha256').update(token).digest('hex')
    createApiKey({ name: 'test-key', keyHash, scopes: ['tasks:read'] })

    // Send a valid MCP initialize request
    const res = await fetch(`${baseUrl}/mcp`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${token}`,
      },
      body: JSON.stringify({
        jsonrpc: '2.0',
        id: 1,
        method: 'initialize',
        params: {
          protocolVersion: '2024-11-05',
          clientInfo: { name: 'test', version: '0.0.1' },
          capabilities: {},
        },
      }),
    })
    // Auth passed — we get a 200 MCP response (or at least not 401)
    expect(res.status).not.toBe(401)
  })
})
