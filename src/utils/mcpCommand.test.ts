import { describe, expect, it } from 'vitest'
import { buildMcpAddCommand, buildMcpJsonConfig, MCP_SERVER_NAME } from './mcpCommand'

describe('buildMcpAddCommand', () => {
  it('embeds origin and token into a claude mcp add one-liner', () => {
    const cmd = buildMcpAddCommand('https://dash.example.com', 'mcp_abc123')
    expect(cmd).toBe(
      'claude mcp add --scope user --transport http dashboard-tasks '
      + 'https://dash.example.com/api/mcp '
      + '--header "Authorization: Bearer mcp_abc123"',
    )
  })

  it('strips a trailing slash on origin so the path is not doubled', () => {
    const cmd = buildMcpAddCommand('http://127.0.0.1:13120/', 'mcp_x')
    expect(cmd).toContain('http://127.0.0.1:13120/api/mcp')
    expect(cmd).not.toContain('//api/mcp')
  })

  it('uses the canonical server name', () => {
    expect(buildMcpAddCommand('http://h', 'mcp_x')).toContain(` ${MCP_SERVER_NAME} `)
  })

  it('strips multiple trailing slashes', () => {
    expect(buildMcpAddCommand('http://h//', 'mcp_x')).toContain('http://h/api/mcp')
  })

  it('rejects an origin with shell metacharacters', () => {
    expect(() => buildMcpAddCommand('http://h"; rm -rf ~', 'mcp_x')).toThrow()
  })
})

describe('buildMcpJsonConfig', () => {
  it('produces valid JSON that round-trips with the expected shape', () => {
    const json = buildMcpJsonConfig('https://dash.example.com', 'mcp_abc123')
    const parsed = JSON.parse(json)
    expect(parsed.mcpServers[MCP_SERVER_NAME]).toEqual({
      type: 'http',
      url: 'https://dash.example.com/api/mcp',
      headers: { Authorization: 'Bearer mcp_abc123' },
    })
  })

  it('strips a trailing slash on origin', () => {
    const parsed = JSON.parse(buildMcpJsonConfig('http://127.0.0.1:13120/', 'mcp_x'))
    expect(parsed.mcpServers[MCP_SERVER_NAME].url).toBe('http://127.0.0.1:13120/api/mcp')
  })

  it('is pretty-printed with 2-space indentation', () => {
    const json = buildMcpJsonConfig('http://h', 'mcp_x')
    expect(json).toContain('\n  "mcpServers"')
  })
})
