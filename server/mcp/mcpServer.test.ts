import type { PipelineOrchestrator } from '../pipeline/orchestrator.js'
import { describe, expect, it } from 'vitest'
import { TOOL_SCOPE_MAP } from './mcpAuth.js'
import { buildMcpServer } from './mcpServer.js'

/**
 * buildMcpServer only uses the orchestrator inside tool *handlers* (called at
 * invocation time, not at registration time), so a bare empty object cast is
 * sufficient for the registration-phase test below.
 */
const mockOrchestrator = {} as unknown as PipelineOrchestrator

function getRegisteredToolNames(): Set<string> {
  const scopes = new Set<never>()
  const server = buildMcpServer(mockOrchestrator, scopes, () => {}, () => {})
  // _registeredTools is a private field but is a plain object at runtime.
  // Casting through `any` is intentional here: we are testing the SDK's
  // internal registration map, which has no public API for listing names.
  const tools = (server as unknown as { _registeredTools: Record<string, unknown> })._registeredTools
  return new Set(Object.keys(tools))
}

describe('tOOL_SCOPE_MAP / buildMcpServer sync', () => {
  it('every tool registered in buildMcpServer has an entry in TOOL_SCOPE_MAP', () => {
    const registeredNames = getRegisteredToolNames()
    const unmapped = [...registeredNames].filter(name => !(name in TOOL_SCOPE_MAP))
    expect(unmapped).toEqual([])
  })

  it('every key in TOOL_SCOPE_MAP corresponds to a registered tool in buildMcpServer', () => {
    const registeredNames = getRegisteredToolNames()
    const unregistered = Object.keys(TOOL_SCOPE_MAP).filter(name => !registeredNames.has(name))
    expect(unregistered).toEqual([])
  })

  it('tOOL_SCOPE_MAP and buildMcpServer register exactly the same set of tool names', () => {
    const registeredNames = getRegisteredToolNames()
    const scopeMapNames = new Set(Object.keys(TOOL_SCOPE_MAP))
    expect(registeredNames).toEqual(scopeMapNames)
  })

  it('registers add_dependency and remove_dependency tools', () => {
    const registeredNames = getRegisteredToolNames()
    expect(registeredNames).toContain('add_dependency')
    expect(registeredNames).toContain('remove_dependency')
  })
})
