import type express from 'express'
import type { AddressInfo } from 'node:net'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import process from 'node:process'
import expressLib from 'express'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { closeDb, getDb } from '../db/client.js'
import { createApiKeyRouter } from '../routes/apiKeyRoutes.js'
import { PipelineOrchestrator } from '../pipeline/orchestrator.js'
import { createMcpRouter } from './mcpRouter.js'

let tmpDir: string
let server: ReturnType<express.Express['listen']>
let baseUrl: string
let orchestrator: PipelineOrchestrator

beforeEach(async () => {
  tmpDir = mkdtempSync(join(tmpdir(), 'mcp-integration-test-'))
  process.env.DASHBOARD_DB_PATH = join(tmpDir, 'test.db')
  getDb()

  orchestrator = new PipelineOrchestrator()
  const app = expressLib()
  app.use(expressLib.json())
  app.use('/api', createApiKeyRouter({ rejectCrossOrigin: () => false }))
  app.use('/api', createMcpRouter(orchestrator, () => {}, () => {}))

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

async function createAdminKey(name: string): Promise<string> {
  const res = await fetch(`${baseUrl}/settings/api-keys`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, scopes: ['keys:manage'] }),
  })
  expect(res.status).toBe(201)
  const { token } = await res.json() as { token: string }
  return token
}

/**
 * Parse a JSON-RPC response from an MCP endpoint.
 * The transport may return either plain application/json or an SSE stream
 * (text/event-stream). In both cases we extract the JSON-RPC payload.
 */
async function parseMcpResponse(res: Response): Promise<unknown> {
  const contentType = res.headers.get('content-type') ?? ''
  const text = await res.text()

  if (contentType.includes('text/event-stream')) {
    // SSE format: lines like "data: {...}\n\n" — extract the first data line
    for (const line of text.split('\n')) {
      const trimmed = line.trim()
      if (trimmed.startsWith('data: ')) {
        return JSON.parse(trimmed.slice(6))
      }
    }
    throw new Error(`No data line found in SSE response: ${text}`)
  }

  return JSON.parse(text)
}

async function mcpCall(
  token: string,
  method: string,
  params: Record<string, unknown>,
): Promise<{ status: number, body: unknown }> {
  const res = await fetch(`${baseUrl}/mcp`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Accept': 'application/json, text/event-stream',
      'Authorization': `Bearer ${token}`,
    },
    body: JSON.stringify({
      jsonrpc: '2.0',
      method,
      id: 1,
      params,
    }),
  })
  const body = await parseMcpResponse(res)
  return { status: res.status, body }
}

async function mcpToolCall(
  token: string,
  toolName: string,
  args: Record<string, unknown>,
): Promise<{ status: number, body: unknown }> {
  return mcpCall(token, 'tools/call', { name: toolName, arguments: args })
}

describe('MCP integration', () => {
  it('creates an admin API key via REST and uses it to initialize MCP', async () => {
    const token = await createAdminKey('integration-admin')

    const { status, body } = await mcpCall(token, 'initialize', {
      protocolVersion: '2024-11-05',
      capabilities: {},
      clientInfo: { name: 'test', version: '1.0' },
    })

    expect(status).toBe(200)
    // MCP initialize response should contain a result (not an error)
    const resp = body as { result?: unknown, error?: unknown }
    expect(resp.result).toBeDefined()
    expect(resp.error).toBeUndefined()
  })

  it('creates a task via MCP create_task and retrieves it via get_task', async () => {
    const token = await createAdminKey('integration-admin-rw')

    // Create a task
    const createRes = await mcpToolCall(token, 'create_task', {
      slug: 'test-task',
      title: 'Test Task',
      cwd: '/tmp',
    })
    expect(createRes.status).toBe(200)

    const createBody = createRes.body as { result?: { content?: Array<{ type: string, text: string }> }, error?: unknown }
    expect(createBody.error).toBeUndefined()
    expect(createBody.result?.content).toBeDefined()

    const createdTask = JSON.parse(createBody.result!.content![0].text) as { id: string, slug: string }
    expect(createdTask.slug).toBe('test-task')
    expect(typeof createdTask.id).toBe('string')

    // Retrieve it via get_task
    const getRes = await mcpToolCall(token, 'get_task', { id_or_slug: 'test-task' })
    expect(getRes.status).toBe(200)

    const getBody = getRes.body as { result?: { content?: Array<{ type: string, text: string }> }, error?: unknown }
    expect(getBody.error).toBeUndefined()

    const fetchedTask = JSON.parse(getBody.result!.content![0].text) as { id: string, slug: string }
    expect(fetchedTask.id).toBe(createdTask.id)
    expect(fetchedTask.slug).toBe('test-task')
  })

  it('returns MCP error "Insufficient scope" when an operator-scoped key calls list_api_keys', async () => {
    // Create a key with only tasks:read scope (operator-level, not admin)
    const operatorRes = await fetch(`${baseUrl}/settings/api-keys`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: 'operator-key', scopes: ['tasks:read'] }),
    })
    expect(operatorRes.status).toBe(201)
    const { token: operatorToken } = await operatorRes.json() as { token: string }

    const { status, body } = await mcpToolCall(operatorToken, 'list_api_keys', {})
    expect(status).toBe(200)

    // The MCP layer returns 200 but the body contains an error result
    const resp = body as { result?: unknown, error?: { message?: string, code?: number } }
    // MCP errors can come as top-level error or as error content in result
    const hasTopLevelError = resp.error !== undefined
    const resultContent = (resp.result as { content?: Array<{ type: string, text: string }> } | undefined)?.content
    const hasErrorInContent = resultContent?.some(c => c.type === 'text' && c.text.includes('Insufficient scope'))

    expect(hasTopLevelError || hasErrorInContent).toBe(true)
    if (hasTopLevelError) {
      expect(resp.error!.message).toMatch(/Insufficient scope/i)
    }
  })
})
