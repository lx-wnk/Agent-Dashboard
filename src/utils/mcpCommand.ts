function validateServerName(serverName: string): void {
  if (!/^[a-z0-9][a-z0-9-]*$/i.test(serverName))
    throw new Error('Unexpected server name')
}

function validateEndpoint(endpoint: string): void {
  if (!/^\/[a-z0-9/_-]*$/i.test(endpoint))
    throw new Error('Unexpected endpoint path')
}

function mcpUrl(origin: string, endpoint: string): string {
  const trimmed = origin.replace(/\/+$/, '')
  if (!/^https?:\/\/[a-z0-9.\-_]+(:\d+)?$/i.test(trimmed))
    throw new Error(`Unexpected origin format: ${trimmed}`)
  return `${trimmed}${endpoint}`
}

export function buildMcpAddCommand(origin: string, token: string, serverName: string, endpoint: string): string {
  validateServerName(serverName)
  validateEndpoint(endpoint)
  return `claude mcp add --scope user --transport http ${serverName} `
    + `${mcpUrl(origin, endpoint)} `
    + `--header "Authorization: Bearer ${token}"`
}

export function buildMcpJsonConfig(origin: string, token: string, serverName: string, endpoint: string): string {
  validateServerName(serverName)
  validateEndpoint(endpoint)
  return JSON.stringify(
    {
      mcpServers: {
        [serverName]: {
          type: 'http',
          url: mcpUrl(origin, endpoint),
          headers: { Authorization: `Bearer ${token}` },
        },
      },
    },
    null,
    2,
  )
}
