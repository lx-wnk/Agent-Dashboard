package mcp

import (
	"encoding/json"
	"net/http"
)

const protocolVersion = "2024-11-05"

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCPHandler returns a chi-compatible http.HandlerFunc for POST /api/mcp.
// It handles: initialize, tools/list, tools/call.
func MCPHandler(registry ToolRegistry) http.HandlerFunc {
	toolsList := buildToolsList(registry)
	return func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeRPC(w, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}})
			return
		}

		switch req.Method {
		case "initialize":
			writeRPC(w, rpcResponse{
				JSONRPC: "2.0", ID: req.ID,
				Result: map[string]any{
					"protocolVersion": protocolVersion,
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]any{"name": "dashboard-tasks", "version": "1.0.0"},
				},
			})

		case "tools/list":
			writeRPC(w, rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": toolsList}})

		case "tools/call":
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				writeRPC(w, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32602, Message: "invalid params"}})
				return
			}
			def, ok := registry[p.Name]
			if !ok {
				writeRPC(w, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: "tool not found: " + p.Name}})
				return
			}
			// Scope enforcement per tool
			auth := AuthFromContext(r.Context())
			requiredScope := ToolScopeMap[p.Name]
			if auth == nil || !auth.Scopes[requiredScope] {
				writeRPC(w, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32003, Message: "Insufficient scope: requires " + requiredScope}})
				return
			}
			if p.Arguments == nil {
				p.Arguments = map[string]any{}
			}
			result, err := def.Handler(r.Context(), p.Arguments)
			if err != nil {
				writeRPC(w, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32003, Message: err.Error()}})
				return
			}
			writeRPC(w, rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result})

		default:
			writeRPC(w, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: "method not found: " + req.Method}})
		}
	}
}

func writeRPC(w http.ResponseWriter, resp rpcResponse) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// buildToolsList builds the tools/list payload once at startup.
func buildToolsList(registry ToolRegistry) []map[string]any {
	out := make([]map[string]any, 0, len(registry))
	for _, def := range registry {
		out = append(out, map[string]any{
			"name":        def.Name,
			"description": def.Description,
			"inputSchema": def.InputSchema,
		})
	}
	return out
}
