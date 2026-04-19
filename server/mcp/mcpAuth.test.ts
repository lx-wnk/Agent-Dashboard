import { describe, expect, it } from 'vitest'
import { resolveScopes, TOOL_SCOPE_MAP } from './mcpAuth.js'

describe('resolveScopes', () => {
  it('tasks:read implies nothing extra', () => {
    expect(resolveScopes(['tasks:read'])).toEqual(new Set(['tasks:read']))
  })

  it('tasks:write implies tasks:read', () => {
    const s = resolveScopes(['tasks:write'])
    expect(s.has('tasks:write')).toBe(true)
    expect(s.has('tasks:read')).toBe(true)
  })

  it('pipeline:control implies tasks:read', () => {
    const s = resolveScopes(['pipeline:control'])
    expect(s.has('pipeline:control')).toBe(true)
    expect(s.has('tasks:read')).toBe(true)
    expect(s.has('tasks:write')).toBe(false)
  })

  it('keys:manage implies all scopes', () => {
    const s = resolveScopes(['keys:manage'])
    expect(s.has('keys:manage')).toBe(true)
    expect(s.has('tasks:read')).toBe(true)
    expect(s.has('tasks:write')).toBe(true)
    expect(s.has('pipeline:control')).toBe(true)
  })

  it('deduplicates when both write and read are explicit', () => {
    const s = resolveScopes(['tasks:read', 'tasks:write'])
    expect([...s].filter(x => x === 'tasks:read')).toHaveLength(1)
  })
})

describe('tOOL_SCOPE_MAP', () => {
  it('maps list_tasks to tasks:read', () => {
    expect(TOOL_SCOPE_MAP.list_tasks).toBe('tasks:read')
  })

  it('maps create_task to tasks:write', () => {
    expect(TOOL_SCOPE_MAP.create_task).toBe('tasks:write')
  })

  it('maps progress_task to pipeline:control', () => {
    expect(TOOL_SCOPE_MAP.progress_task).toBe('pipeline:control')
  })

  it('maps create_api_key to keys:manage', () => {
    expect(TOOL_SCOPE_MAP.create_api_key).toBe('keys:manage')
  })
})
