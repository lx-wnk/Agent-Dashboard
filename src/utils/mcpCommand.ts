const SERVER_NAME_RE = /^[a-z0-9][a-z0-9-]*$/i
const ENDPOINT_RE = /^\/[\w/-]*$/
const TRAILING_SLASH_RE = /\/+$/
const ORIGIN_RE = /^https?:\/\/[\w.\-]+(?::\d+)?$/i

function validateServerName(serverName: string): void {
  if (!SERVER_NAME_RE.test(serverName))
    throw new Error('Unexpected server name')
}

function validateEndpoint(endpoint: string): void {
  if (!ENDPOINT_RE.test(endpoint))
    throw new Error('Unexpected endpoint path')
}

function mcpUrl(origin: string, endpoint: string): string {
  const trimmed = origin.replace(TRAILING_SLASH_RE, '')
  if (!ORIGIN_RE.test(trimmed))
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
