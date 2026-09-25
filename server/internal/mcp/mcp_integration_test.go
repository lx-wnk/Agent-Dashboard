package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lx-wnk/kontor/server/internal/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPEndpoint_Initialize(t *testing.T) {
	h := mcp.MCPHandler(mcp.ToolRegistry{}, nil, nil, nil)
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/mcp", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	result := resp["result"].(map[string]any)
	require.Equal(t, "2024-11-05", result["protocolVersion"])
	info := result["serverInfo"].(map[string]any)
	require.Equal(t, mcp.ServerName, info["name"])
}

func TestMCPEndpoint_ToolsList_SortedAlphabetically(t *testing.T) {
	noop := func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
		return mcp.OK(nil)
	}
	registry := mcp.ToolRegistry{}
	// Use real scope-map names that sort correctly: "list_tasks" < "update_task"
	registry.Register(&mcp.ToolDef{Name: "update_task", Description: "Z", InputSchema: map[string]any{"type": "object"}, Handler: noop})
	registry.Register(&mcp.ToolDef{Name: "list_tasks", Description: "A", InputSchema: map[string]any{"type": "object"}, Handler: noop})
	h := mcp.MCPHandler(registry, nil, nil, nil)
	body := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
	req := httptest.NewRequest(http.MethodPost, "/api/mcp", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	toolsList := resp["result"].(map[string]any)["tools"].([]any)
	require.Equal(t, "list_tasks", toolsList[0].(map[string]any)["name"])
	require.Equal(t, "update_task", toolsList[1].(map[string]any)["name"])
}

func TestMCPEndpoint_ToolsCall_MissingScope(t *testing.T) {
	registry := mcp.ToolRegistry{}
	registry.Register(&mcp.ToolDef{
		Name:        "list_tasks",
		Description: "test",
		InputSchema: map[string]any{"type": "object"},
		Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
			return mcp.OK([]string{})
		},
	})
	h := mcp.MCPHandler(registry, nil, nil, nil)
	// No auth in context — scope check fails with -32003
	body := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_tasks","arguments":{}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/mcp", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	rpcErr := resp["error"].(map[string]any)
	require.EqualValues(t, -32003, rpcErr["code"])
}

func TestMCPEndpoint_InitializeDeclaresListChanged(t *testing.T) {
	notifier := mcp.NewNotifier()
	h := mcp.MCPHandler(mcp.ToolRegistry{}, nil, nil, notifier)

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize"}`
	req := httptest.NewRequest(http.MethodPost, "/api/mcp", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp struct {
		Result struct {
			Capabilities struct {
				Tools struct {
					ListChanged bool `json:"listChanged"`
				} `json:"tools"`
			} `json:"capabilities"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.True(t, resp.Result.Capabilities.Tools.ListChanged, "with a notifier, listChanged must be true")

	// Without notifier
	hNoNotifier := mcp.MCPHandler(mcp.ToolRegistry{}, nil, nil, nil)
	req2 := httptest.NewRequest(http.MethodPost, "/api/mcp", strings.NewReader(body))
	rec2 := httptest.NewRecorder()
	hNoNotifier.ServeHTTP(rec2, req2)

	var resp2 struct {
		Result struct {
			Capabilities struct {
				Tools struct {
					ListChanged bool `json:"listChanged"`
				} `json:"tools"`
			} `json:"capabilities"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp2))
	assert.False(t, resp2.Result.Capabilities.Tools.ListChanged, "without notifier, listChanged must be false")
}

func TestMCPEndpoint_GETStreamsToolsListChangedNotification(t *testing.T) {
	notifier := mcp.NewNotifier()
	h := mcp.MCPHandler(mcp.ToolRegistry{}, nil, nil, notifier)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/mcp", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		h.ServeHTTP(rec, req)
		close(done)
	}()

	// Give the handler a moment to subscribe
	time.Sleep(50 * time.Millisecond)
	notifier.NotifyToolsChanged()
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), "notifications/tools/list_changed")
}
