import { describe, expect, it } from 'vitest'
import { buildMcpAddCommand, buildMcpJsonConfig } from './mcpCommand'

describe('buildMcpAddCommand', () => {
  it('embeds origin and token into a claude mcp add one-liner', () => {
    const cmd = buildMcpAddCommand('https://dash.example.com', 'mcp_abc123', 'dashboard-tasks', '/api/mcp')
    expect(cmd).toBe(
      'claude mcp add --scope user --transport http dashboard-tasks '
      + 'https://dash.example.com/api/mcp '
      + '--header "Authorization: Bearer mcp_abc123"',
    )
  })

  it('strips a trailing slash on origin so the path is not doubled', () => {
    const cmd = buildMcpAddCommand('http://127.0.0.1:13120/', 'mcp_x', 'dashboard-tasks', '/api/mcp')
    expect(cmd).toContain('http://127.0.0.1:13120/api/mcp')
    expect(cmd).not.toContain('//api/mcp')
  })

  it('uses the passed serverName in the command', () => {
    expect(buildMcpAddCommand('http://h', 'mcp_x', 'dashboard-tasks', '/api/mcp')).toContain(' dashboard-tasks ')
  })

  it('strips multiple trailing slashes', () => {
    expect(buildMcpAddCommand('http://h//', 'mcp_x', 'dashboard-tasks', '/api/mcp')).toContain('http://h/api/mcp')
  })

  it('rejects an origin with shell metacharacters', () => {
    expect(() => buildMcpAddCommand('http://h"; rm -rf ~', 'mcp_x', 'dashboard-tasks', '/api/mcp')).toThrow()
  })

  it('rejects a serverName with spaces or special chars', () => {
    expect(() => buildMcpAddCommand('http://h', 'tok', 'bad name', '/api/mcp')).toThrow('Unexpected server name')
  })

  it('rejects an endpoint without a leading slash', () => {
    expect(() => buildMcpAddCommand('http://h', 'tok', 'dashboard-tasks', 'no-leading-slash')).toThrow('Unexpected endpoint path')
  })
})

describe('buildMcpJsonConfig', () => {
  it('produces valid JSON that round-trips with the expected shape', () => {
    const json = buildMcpJsonConfig('https://dash.example.com', 'mcp_abc123', 'dashboard-tasks', '/api/mcp')
    const parsed = JSON.parse(json)
    expect(parsed.mcpServers['dashboard-tasks']).toEqual({
      type: 'http',
      url: 'https://dash.example.com/api/mcp',
      headers: { Authorization: 'Bearer mcp_abc123' },
    })
  })

  it('strips a trailing slash on origin', () => {
    const parsed = JSON.parse(buildMcpJsonConfig('http://127.0.0.1:13120/', 'mcp_x', 'dashboard-tasks', '/api/mcp'))
    expect(parsed.mcpServers['dashboard-tasks'].url).toBe('http://127.0.0.1:13120/api/mcp')
  })

  it('is pretty-printed with 2-space indentation', () => {
    const json = buildMcpJsonConfig('http://h', 'mcp_x', 'dashboard-tasks', '/api/mcp')
    expect(json).toContain('\n  "mcpServers"')
  })
})
