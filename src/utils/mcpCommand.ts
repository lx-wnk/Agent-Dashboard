/** MCP server name — must stay in sync with serverInfo.name in server/internal/mcp/jsonrpc.go. */
export const MCP_SERVER_NAME = 'dashboard-tasks'

function mcpUrl(origin: string): string {
  const trimmed = origin.replace(/\/+$/, '')
  if (!/^https?:\/\/[a-z0-9.\-_]+(:\d+)?$/i.test(trimmed))
    throw new Error(`Unexpected origin format: ${trimmed}`)
  return `${trimmed}/api/mcp`
}

export function buildMcpAddCommand(origin: string, token: string): string {
  return `claude mcp add --scope user --transport http ${MCP_SERVER_NAME} `
    + `${mcpUrl(origin)} `
    + `--header "Authorization: Bearer ${token}"`
}

export function buildMcpJsonConfig(origin: string, token: string): string {
  return JSON.stringify(
    {
      mcpServers: {
        [MCP_SERVER_NAME]: {
          type: 'http',
          url: mcpUrl(origin),
          headers: { Authorization: `Bearer ${token}` },
        },
      },
    },
    null,
    2,
  )
}
