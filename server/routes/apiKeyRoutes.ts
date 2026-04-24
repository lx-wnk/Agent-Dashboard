import type express from 'express'
import type { McpScope } from '../../src/types.js'
import { Router } from 'express'
import { createApiKey, generateApiToken, getApiKeyById, hashApiToken, listApiKeys, revokeApiKey } from '../db/apiKeysRepo.js'

type RejectCrossOrigin = (req: express.Request, res: express.Response) => boolean

export interface ApiKeyRouterDeps {
  rejectCrossOrigin: RejectCrossOrigin
}

const VALID_SCOPES = new Set<McpScope>(['tasks:read', 'tasks:write', 'pipeline:control', 'keys:manage'])

export function createApiKeyRouter(deps: ApiKeyRouterDeps): Router {
  const router = Router()

  // GET /api/settings/api-keys
  router.get('/settings/api-keys', (req, res) => {
    if (deps.rejectCrossOrigin(req, res))
      return
    res.json(listApiKeys({ includeRevoked: false }))
  })

  // POST /api/settings/api-keys
  // Body: { name: string, scopes: McpScope[] }
  // Returns: { key: ApiKey, token: string }  ← token shown once, never again
  router.post('/settings/api-keys', (req, res) => {
    if (deps.rejectCrossOrigin(req, res))
      return
    const { name, scopes } = req.body as { name?: string, scopes?: McpScope[] }

    if (!name || typeof name !== 'string' || !name.trim())
      return void res.status(400).json({ error: 'name is required' })
    if (!Array.isArray(scopes) || scopes.length === 0)
      return void res.status(400).json({ error: 'scopes must be a non-empty array' })

    for (const s of scopes) {
      if (!VALID_SCOPES.has(s))
        return void res.status(400).json({ error: `invalid scope: ${s}` })
    }

    const token = generateApiToken()
    const keyHash = hashApiToken(token)
    try {
      const key = createApiKey({ name: name.trim(), keyHash, scopes })
      res.status(201).json({ key, token })
    }
    catch (err) {
      if (err instanceof Error && (err as NodeJS.ErrnoException & { code?: string }).code === 'SQLITE_CONSTRAINT_UNIQUE')
        return void res.status(409).json({ error: 'An API key with this name already exists' })
      throw err
    }
  })

  // DELETE /api/settings/api-keys/:id
  router.delete('/settings/api-keys/:id', (req, res) => {
    if (deps.rejectCrossOrigin(req, res))
      return
    const key = getApiKeyById(req.params.id)
    if (!key)
      return void res.status(404).json({ error: 'API key not found' })
    revokeApiKey(req.params.id)
    res.status(204).send()
  })

  return router
}
