package mcp

// ServerName is the MCP server identity returned in initialize/serverInfo and
// used by clients to register this server. Single source of truth.
const ServerName = "dashboard-tasks"

// EndpointPath is the HTTP route the MCP JSON-RPC handler is mounted on.
const EndpointPath = "/api/mcp"
