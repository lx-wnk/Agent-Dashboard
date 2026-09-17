import { describe, expect, it } from 'vitest'
import { isApplicationTool } from '../applicationTool'

describe('isApplicationTool', () => {
  it('accepts a namespaced tool from a real MCP application', () => {
    expect(isApplicationTool('mcp__mail__read')).toBe(true)
  })

  it('accepts a tool name that itself contains a separator', () => {
    expect(isApplicationTool('mcp__mail__move__message')).toBe(true)
  })

  it('rejects the reserved dashboard-tasks server', () => {
    expect(isApplicationTool('mcp__dashboard-tasks__list')).toBe(false)
  })

  it('rejects the reserved dashboard-channel server', () => {
    expect(isApplicationTool('mcp__dashboard-channel__reply')).toBe(false)
  })

  it('rejects an empty server segment', () => {
    expect(isApplicationTool('mcp____read')).toBe(false)
  })

  it('rejects an empty tool segment', () => {
    expect(isApplicationTool('mcp__mail__')).toBe(false)
  })

  it('rejects a non-mcp tool', () => {
    expect(isApplicationTool('Bash')).toBe(false)
  })

  it('rejects an empty string', () => {
    expect(isApplicationTool('')).toBe(false)
  })
})
