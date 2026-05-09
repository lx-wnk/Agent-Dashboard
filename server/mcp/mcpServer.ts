import type { McpScope } from '../../src/types.js'
import type { PipelineOrchestrator } from '../pipeline/orchestrator.js'
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { makeToolRegistrar } from './mcpAuth.js'
import { registerControlTools } from './tools/controlTools.js'
import { registerDependencyTools } from './tools/dependencyTools.js'
import { registerKeyTools } from './tools/keyTools.js'
import { registerManageTaskTool } from './tools/manageTaskTools.js'
import { registerReadTools } from './tools/readTools.js'
import { registerWriteTools } from './tools/writeTools.js'

export function buildMcpServer(
  orchestrator: PipelineOrchestrator,
  scopes: Set<McpScope>,
  broadcast: (taskId: string) => void,
  broadcastDeleted: (taskId: string) => void,
  callerUserId: string | null,
): McpServer {
  const server = new McpServer({ name: 'dashboard-tasks', version: '1.0.0' })
  const tool = makeToolRegistrar(server, scopes)

  registerReadTools(tool, callerUserId)
  registerWriteTools(tool, broadcast, broadcastDeleted, callerUserId)
  registerControlTools(tool, orchestrator, broadcast, callerUserId)
  registerManageTaskTool(tool, broadcast, callerUserId)
  registerDependencyTools(tool, broadcast)
  registerKeyTools(tool)

  return server
}
