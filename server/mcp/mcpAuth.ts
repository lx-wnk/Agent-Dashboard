import type { NextFunction, Request, Response } from 'express'
import type { McpScope } from '../../src/types.js'
import { createHash } from 'node:crypto'
import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import type { ZodRawShape } from 'zod'
import { z } from 'zod'
import { getApiKeyByHash, touchApiKey } from '../db/apiKeysRepo.js'

// Runtime scope enforcement: every tool is registered via makeToolRegistrar(),
// which reads the required scope from this map before invoking the handler.
// Adding a tool without an entry here is a compile-time type error.
export const TOOL_SCOPE_MAP: Record<string, McpScope> = {
  // tasks:read
  list_tasks: 'tasks:read',
  get_task: 'tasks:read',
  list_stage_runs: 'tasks:read',
  list_audit: 'tasks:read',
  list_permission_requests: 'tasks:read',
  // tasks:write
  create_task: 'tasks:write',
  update_task: 'tasks:write',
  delete_task: 'tasks:write',
  // pipeline:control
  progress_task: 'pipeline:control',
  approve_task: 'pipeline:control',
  request_changes: 'pipeline:control',
  cancel_task: 'pipeline:control',
  retry_task: 'pipeline:control',
  grant_permission: 'pipeline:control',
  resolve_permission_request: 'pipeline:control',
  // keys:manage
  list_api_keys: 'keys:manage',
  create_api_key: 'keys:manage',
  revoke_api_key: 'keys:manage',
}

const SCOPE_IMPLIES: Record<McpScope, McpScope[]> = {
  'tasks:read': [],
  'tasks:write': ['tasks:read'],
  'pipeline:control': ['tasks:read'],
  'keys:manage': ['tasks:read', 'tasks:write', 'pipeline:control'],
}

export function resolveScopes(scopes: McpScope[]): Set<McpScope> {
  const result = new Set<McpScope>(scopes)
  for (const scope of scopes) {
    for (const implied of SCOPE_IMPLIES[scope] ?? []) {
      result.add(implied)
    }
  }
  return result
}

// Augment Express Request so downstream handlers can read mcpAuth
declare global {
  // eslint-disable-next-line ts/no-namespace
  namespace Express {
    interface Request {
      mcpAuth?: { keyId: string, effectiveScopes: Set<McpScope> }
    }
  }
}

export function mcpAuthMiddleware(req: Request, res: Response, next: NextFunction): void {
  const header = req.headers.authorization
  if (!header || !header.startsWith('Bearer ')) {
    res.status(401).json({ error: 'Missing or invalid Authorization header' })
    return
  }
  const token = header.slice(7).trim()
  const hash = createHash('sha256').update(token).digest('hex')
  const key = getApiKeyByHash(hash)
  if (!key) {
    res.status(401).json({ error: 'Invalid or revoked API key' })
    return
  }
  req.mcpAuth = { keyId: key.id, effectiveScopes: resolveScopes(key.scopes) }
  // Fire-and-forget — last_used_at update must not block the request
  setImmediate(() => touchApiKey(key.id))
  next()
}

type ToolResult = { content: Array<{ type: 'text', text: string }> }

/** Uniform success response — wraps data as JSON text content block. */
export function ok(data: unknown): ToolResult {
  return { content: [{ type: 'text' as const, text: JSON.stringify(data) }] }
}

/** Throws so the MCP SDK surfaces it as a tool error. */
export function mcpError(message: string): never {
  const err = new Error(message) as Error & { code: number }
  err.code = -32003
  throw err
}

/**
 * Returns a tool() helper that automatically enforces the scope declared in
 * TOOL_SCOPE_MAP before invoking the handler. All tools MUST be registered
 * via this helper — direct server.tool() calls bypass scope enforcement.
 */
export function makeToolRegistrar(server: McpServer, scopes: Set<McpScope>) {
  return function tool<S extends ZodRawShape>(
    name: keyof typeof TOOL_SCOPE_MAP,
    schema: S,
    handler: (args: z.infer<z.ZodObject<S>>) => ToolResult | Promise<ToolResult>,
  ): void {
    const needed = TOOL_SCOPE_MAP[name]
    // Cast through unknown to satisfy the SDK's ZodRawShapeCompat overload —
    // ZodRawShape and ZodRawShapeCompat are structurally identical at runtime.
    ;(server.tool as unknown as (
      name: string,
      schema: S,
      cb: (args: z.infer<z.ZodObject<S>>) => ToolResult | Promise<ToolResult>,
    ) => void)(name as string, schema, (args: z.infer<z.ZodObject<S>>) => {
      if (!scopes.has(needed))
        mcpError(`Insufficient scope: requires ${needed}`)
      return handler(args)
    })
  }
}
