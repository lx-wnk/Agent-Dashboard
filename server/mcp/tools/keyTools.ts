import { z } from 'zod'
import type { McpScope } from '../../../src/types.js'
import { createApiKey, generateApiToken, getApiKeyById, hashApiToken, listApiKeys, revokeApiKey } from '../../db/apiKeysRepo.js'
import { mcpError, ok } from '../mcpAuth.js'
import type { makeToolRegistrar } from '../mcpAuth.js'

type ToolFn = ReturnType<typeof makeToolRegistrar>

export function registerKeyTools(tool: ToolFn): void {
  tool(
    'list_api_keys',
    { include_revoked: z.boolean().optional() },
    async ({ include_revoked }) => {
      return ok(listApiKeys({ includeRevoked: include_revoked }))
    },
  )

  tool(
    'create_api_key',
    {
      name: z.string().describe('Unique human-readable name for this key'),
      scopes: z.array(z.enum(['tasks:read', 'tasks:write', 'pipeline:control', 'keys:manage'])),
    },
    async (args) => {
      const token = generateApiToken()
      const keyHash = hashApiToken(token)
      const key = createApiKey({ name: args.name, keyHash, scopes: args.scopes as McpScope[] })
      return ok({ key, token })
    },
  )

  tool(
    'revoke_api_key',
    { id: z.string() },
    async ({ id }) => {
      if (!getApiKeyById(id))
        mcpError(`API key not found: ${id}`)
      revokeApiKey(id)
      return ok({ success: true })
    },
  )
}
