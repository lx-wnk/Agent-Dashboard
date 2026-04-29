import type { PipelineOrchestrator } from '../pipeline/orchestrator.js'
import { StreamableHTTPServerTransport } from '@modelcontextprotocol/sdk/server/streamableHttp.js'
import { Router } from 'express'
import { mcpAuthMiddleware } from './mcpAuth.js'
import { buildMcpServer } from './mcpServer.js'

export function createMcpRouter(
  orchestrator: PipelineOrchestrator,
  broadcast: (taskId: string) => void,
  broadcastDeleted: (taskId: string) => void,
): Router {
  const router = Router()

  router.post('/mcp', mcpAuthMiddleware, async (req, res) => {
    const { effectiveScopes, userId } = req.mcpAuth!
    const server = buildMcpServer(orchestrator, effectiveScopes, broadcast, broadcastDeleted, userId)
    // Stateless mode: sessionIdGenerator: undefined means every POST is
    // self-contained — no server-side session map is maintained.
    const transport = new StreamableHTTPServerTransport({ sessionIdGenerator: undefined })
    await server.connect(transport)
    try {
      await transport.handleRequest(req, res, req.body)
    }
    finally {
      await server.close()
    }
  })

  return router
}
