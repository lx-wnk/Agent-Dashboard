/** Canonical MCP server name — matches the server's own serverInfo.name. */
export const MCP_SERVER_NAME = 'dashboard-tasks'

function mcpUrl(origin: string): string {
  return `${origin.replace(/\/+$/, '')}/api/mcp`
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
