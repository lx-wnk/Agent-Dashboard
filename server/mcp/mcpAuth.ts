import type { NextFunction, Request, Response } from 'express'
import type { McpScope } from '../../src/types.js'
import { createHash } from 'node:crypto'
import { getApiKeyByHash, touchApiKey } from '../db/apiKeysRepo.js'

// Documentation of the scope each MCP tool requires.
// NOTE: This map is NOT wired into runtime enforcement — each tool handler
// calls requireScope() directly. Keep this map in sync with mcpServer.ts
// for auditing and tests; a drift here indicates a real mismatch to investigate.
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
