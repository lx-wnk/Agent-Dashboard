package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lx-wnk/kontor/server/internal/mcp"
)

type stubModuleTools struct {
	tools  []mcp.ModuleTool
	called string
}

func (s *stubModuleTools) List(context.Context) []mcp.ModuleTool { return s.tools }

func (s *stubModuleTools) Call(_ context.Context, moduleID, tool string, _ map[string]any) (string, error) {
	s.called = moduleID + "/" + tool
	return "from the module", nil
}

// rpc calls the handler as a caller holding the given scopes.
func rpc(t *testing.T, h http.HandlerFunc, method string, params any, scopes ...string) map[string]any {
	t.Helper()
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	granted := make(map[string]bool, len(scopes))
	for _, s := range scopes {
		granted[s] = true
	}
	req := httptest.NewRequest(http.MethodPost, "/api/mcp", bytes.NewReader(body))
	req = req.WithContext(mcp.ContextWithAuth(req.Context(), &mcp.MCPAuthInfo{KeyID: "k", Scopes: granted}))
	rr := httptest.NewRecorder()
	h(rr, req)
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal %q: %v", rr.Body.String(), err)
	}
	return out
}

func toolNames(t *testing.T, resp map[string]any) []string {
	t.Helper()
	result, _ := resp["result"].(map[string]any)
	raw, _ := result["tools"].([]any)
	names := make([]string, 0, len(raw))
	for _, entry := range raw {
		m, _ := entry.(map[string]any)
		name, _ := m["name"].(string)
		names = append(names, name)
	}
	return names
}

// A module's tools reach the agent under the module's name, and they are asked
// for per request: a module that stopped between two calls must simply not be
// in the list, rather than be listed and then fail when called.
func TestMCPHandler_ListsModuleToolsUnderTheirNamespace(t *testing.T) {
	src := &stubModuleTools{tools: []mcp.ModuleTool{
		{ModuleID: "obsidian", Name: "search", Description: "search the vault"},
	}}
	h := mcp.MCPHandler(mcp.ToolRegistry{}, src)

	names := toolNames(t, rpc(t, h, "tools/list", map[string]any{}, "module:obsidian:search"))
	if len(names) != 1 || names[0] != "obsidian__search" {
		t.Fatalf("tools = %v, want the namespaced module tool", names)
	}

	src.tools = nil
	if names := toolNames(t, rpc(t, h, "tools/list", map[string]any{}, "module:obsidian:search")); len(names) != 0 {
		t.Fatalf("tools = %v, want none once the module is gone", names)
	}
}

func TestMCPHandler_RoutesAModuleToolCall(t *testing.T) {
	src := &stubModuleTools{tools: []mcp.ModuleTool{{ModuleID: "obsidian", Name: "search"}}}
	h := mcp.MCPHandler(mcp.ToolRegistry{}, src)

	resp := rpc(t, h, "tools/call", map[string]any{"name": "obsidian__search", "arguments": map[string]any{"q": "x"}}, "module:obsidian:search")
	if resp["error"] != nil {
		t.Fatalf("call returned an error: %v", resp["error"])
	}
	if src.called != "obsidian/search" {
		t.Errorf("routed to %q, want obsidian/search", src.called)
	}
}

// A qualified name whose module is not listed must read as an unknown tool,
// not as a call attempted against nothing.
func TestMCPHandler_UnknownModuleToolIsNotFound(t *testing.T) {
	h := mcp.MCPHandler(mcp.ToolRegistry{}, &stubModuleTools{})
	resp := rpc(t, h, "tools/call", map[string]any{"name": "gone__search"})
	if resp["error"] == nil {
		t.Fatal("a tool nobody offers must be reported as not found")
	}
}

// Default deny: a module tool is invisible and uncallable until the caller has
// been granted that one tool. Granting "the module" wholesale is exactly the
// coarse permission this avoids.
func TestMCPHandler_ModuleToolIsDeniedWithoutItsOwnScope(t *testing.T) {
	src := &stubModuleTools{tools: []mcp.ModuleTool{{ModuleID: "obsidian", Name: "write"}}}
	h := mcp.MCPHandler(mcp.ToolRegistry{}, src)

	if names := toolNames(t, rpc(t, h, "tools/list", map[string]any{}, "module:obsidian:search")); len(names) != 0 {
		t.Errorf("tools = %v, want none: the caller may search, not write", names)
	}

	resp := rpc(t, h, "tools/call", map[string]any{"name": "obsidian__write"}, "module:obsidian:search")
	if resp["error"] == nil {
		t.Fatal("calling an ungranted module tool must be refused")
	}
	if src.called != "" {
		t.Errorf("the module was called anyway: %q", src.called)
	}
}
