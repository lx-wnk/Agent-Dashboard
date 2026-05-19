package tools

import (
	"testing"

	"github.com/stretchr/testify/require"

	mcp "github.com/lx-wnk/agent-dashboard/server/internal/mcp"
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
