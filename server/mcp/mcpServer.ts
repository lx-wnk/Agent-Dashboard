import type { McpScope } from '../../src/types.js'
import type { PipelineOrchestrator } from '../pipeline/orchestrator.js'
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { makeToolRegistrar } from './mcpAuth.js'
import { registerControlTools } from './tools/controlTools.js'
import { registerKeyTools } from './tools/keyTools.js'
import { registerReadTools } from './tools/readTools.js'
import { registerWriteTools } from './tools/writeTools.js'

export function buildMcpServer(
  orchestrator: PipelineOrchestrator,
  scopes: Set<McpScope>,
  broadcast: (taskId: string) => void,
  broadcastDeleted: (taskId: string) => void,
): McpServer {
  const server = new McpServer({ name: 'dashboard-tasks', version: '1.0.0' })
  const tool = makeToolRegistrar(server, scopes)

  registerReadTools(tool)
  registerWriteTools(tool, broadcast, broadcastDeleted)
  registerControlTools(tool, orchestrator, broadcast)
  registerKeyTools(tool)

  return server
}
