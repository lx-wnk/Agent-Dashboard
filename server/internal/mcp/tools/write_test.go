package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	mcp "github.com/lx-wnk/agent-dashboard/server/internal/mcp"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

func TestRegisterWriteTools_AllToolsPresent(t *testing.T) {
	registry := mcp.ToolRegistry{}
	RegisterWriteTools(registry, WriteDeps{})
	for _, name := range []string{
		"create_task",
		"update_task",
		"delete_task",
		"manage_task",
		"add_dependency",
		"remove_dependency",
	} {
		if _, ok := registry[name]; !ok {
			t.Errorf("expected tool %q to be registered, but it was not", name)
		}
	}
}

// --- parsePermissionsArg ---

func TestParsePermissionsArg_AbsentKey(t *testing.T) {
	result, err := parsePermissionsArg(map[string]any{})
	require.NoError(t, err)
	require.Nil(t, result)
}

func TestParsePermissionsArg_NilValue(t *testing.T) {
	result, err := parsePermissionsArg(map[string]any{"permissions": nil})
	require.NoError(t, err)
	require.Nil(t, result)
}

func TestParsePermissionsArg_NotArray(t *testing.T) {
	_, err := parsePermissionsArg(map[string]any{"permissions": "string"})
	require.Error(t, err)
	require.ErrorContains(t, err, "must be an array")
}

func TestParsePermissionsArg_MissingTool(t *testing.T) {
	args := map[string]any{
		"permissions": []any{map[string]any{"pattern": "x"}},
	}
	_, err := parsePermissionsArg(args)
	require.Error(t, err)
	require.ErrorContains(t, err, "missing tool")
}

func TestParsePermissionsArg_ValidEntry(t *testing.T) {
	args := map[string]any{
		"permissions": []any{map[string]any{"tool": "Read", "pattern": "src/*.go"}},
	}
	result, err := parsePermissionsArg(args)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, "Read", result[0].Tool)
	require.NotNil(t, result[0].Pattern)
	require.Equal(t, "src/*.go", *result[0].Pattern)
}

func TestParsePermissionsArg_ExpiresAt(t *testing.T) {
	args := map[string]any{
		"permissions": []any{map[string]any{"tool": "Read", "expiresAt": "2099-01-01T00:00:00Z"}},
	}
	result, err := parsePermissionsArg(args)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.NotNil(t, result[0].ExpiresAt)
	require.Equal(t, "2099-01-01T00:00:00Z", *result[0].ExpiresAt)
}

// --- permInputsToGrantEntries ---

func TestPermInputsToGrantEntries_UnknownTool(t *testing.T) {
	_, err := permInputsToGrantEntries([]permissionInput{{Tool: "NotARealTool"}})
	require.Error(t, err)
	require.ErrorContains(t, err, "tool not in allow-list")
}

func TestPermInputsToGrantEntries_ValidTool(t *testing.T) {
	entries, err := permInputsToGrantEntries([]permissionInput{{Tool: "Read"}})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "Read", entries[0].Tool)
}

func TestPermInputsToGrantEntries_ValidToolWithPattern(t *testing.T) {
	entries, err := permInputsToGrantEntries([]permissionInput{{Tool: "Bash", Pattern: strPtr("pnpm test*")}})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.NotNil(t, entries[0].Pattern)
	require.Equal(t, "pnpm test*", *entries[0].Pattern)
}

func TestPermInputsToGrantEntries_InvalidExpiresAt(t *testing.T) {
	_, err := permInputsToGrantEntries([]permissionInput{{Tool: "Read", ExpiresAt: strPtr("not-a-date")}})
	require.Error(t, err)
	require.ErrorContains(t, err, "invalid expiresAt format")
}

func TestPermInputsToGrantEntries_ValidExpiresAt(t *testing.T) {
	entries, err := permInputsToGrantEntries([]permissionInput{{Tool: "Read", ExpiresAt: strPtr("2099-06-01T12:00:00Z")}})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.NotNil(t, entries[0].ExpiresAt)
}

func strPtr(s string) *string { return &s }

// --- create_task / update_task autonomy ---

func newWriteDepsForTest(t *testing.T) WriteDeps {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	client := bundle.Client
	return WriteDeps{
		TaskRepo:  repo.NewTaskRepo(client),
		PermRepo:  repo.NewPermissionRepo(client),
		AuditRepo: repo.NewAuditEventRepo(client),
	}
}

// toolResultJSON unmarshals the first content block text from a ToolResult.
func toolResultJSON(t *testing.T, result *mcp.ToolResult) map[string]any {
	t.Helper()
	require.NotEmpty(t, result.Content)
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Content[0].Text), &m))
	return m
}

func invokeCreateTask(t *testing.T, registry mcp.ToolRegistry, args map[string]any) (map[string]any, error) {
	t.Helper()
	tool, ok := registry["create_task"]
	require.True(t, ok, "create_task not registered")
	result, err := tool.Handler(context.Background(), args)
	if err != nil {
		return nil, err
	}
	return toolResultJSON(t, result), nil
}

func invokeUpdateTask(t *testing.T, registry mcp.ToolRegistry, args map[string]any) (map[string]any, error) {
	t.Helper()
	tool, ok := registry["update_task"]
	require.True(t, ok, "update_task not registered")
	result, err := tool.Handler(context.Background(), args)
	if err != nil {
		return nil, err
	}
	return toolResultJSON(t, result), nil
}

func TestCreateTask_Autonomy_Persists(t *testing.T) {
	deps := newWriteDepsForTest(t)
	registry := mcp.ToolRegistry{}
	RegisterWriteTools(registry, deps)

	out, err := invokeCreateTask(t, registry, map[string]any{
		"slug":     "aut-mcp-full",
		"title":    "Full Autonomy MCP Task",
		"cwd":      "/tmp/aut-mcp-full",
		"autonomy": "full",
	})
	require.NoError(t, err)

	taskMap, _ := out["task"].(map[string]any)
	require.NotNil(t, taskMap, "response must contain task")
	require.Equal(t, "full", taskMap["autonomy"], "autonomy must be persisted")
}

func TestCreateTask_Autonomy_InvalidValue_ReturnsError(t *testing.T) {
	deps := newWriteDepsForTest(t)
	registry := mcp.ToolRegistry{}
	RegisterWriteTools(registry, deps)

	_, err := invokeCreateTask(t, registry, map[string]any{
		"slug":     "aut-mcp-bad",
		"title":    "Bad Autonomy",
		"cwd":      "/tmp/aut-mcp-bad",
		"autonomy": "superuser",
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "invalid autonomy value")
}

func TestUpdateTask_Autonomy_Persists(t *testing.T) {
	deps := newWriteDepsForTest(t)
	registry := mcp.ToolRegistry{}
	RegisterWriteTools(registry, deps)

	// Create task first.
	out, err := invokeCreateTask(t, registry, map[string]any{
		"slug":  "aut-mcp-upd",
		"title": "Update Autonomy MCP",
		"cwd":   "/tmp/aut-mcp-upd",
	})
	require.NoError(t, err)
	taskMap, _ := out["task"].(map[string]any)
	require.NotNil(t, taskMap)
	id, _ := taskMap["id"].(string)
	require.NotEmpty(t, id)

	// Update autonomy.
	updated, err := invokeUpdateTask(t, registry, map[string]any{
		"id":       id,
		"autonomy": "manual",
	})
	require.NoError(t, err)
	require.Equal(t, "manual", updated["autonomy"], "autonomy must be updated")
}
