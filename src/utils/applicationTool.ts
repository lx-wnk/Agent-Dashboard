// Twin of server/internal/mcpapps/catalogue.go's IsApplicationTool — keep both in sync by hand.
const RESERVED_SERVERS = new Set(['dashboard-channel', 'dashboard-tasks'])

export function isApplicationTool(tool: string): boolean {
  if (!tool.startsWith('mcp__'))
    return false
  // Only the FIRST separator after the server splits the name: a tool name may
  // itself contain "__", exactly as the Go twin's strings.Cut does.
  const rest = tool.slice('mcp__'.length)
  const sep = rest.indexOf('__')
  if (sep <= 0 || sep + 2 >= rest.length)
    return false
  return !RESERVED_SERVERS.has(rest.slice(0, sep))
}
