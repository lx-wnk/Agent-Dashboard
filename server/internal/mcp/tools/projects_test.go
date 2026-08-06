package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	mcp "github.com/lx-wnk/agent-dashboard/server/internal/mcp"
)

func invokeCreateProject(t *testing.T, registry mcp.ToolRegistry, args map[string]any) (map[string]any, error) {
	t.Helper()
	tool, ok := registry["create_project"]
	require.True(t, ok, "create_project not registered")
	result, err := tool.Handler(context.Background(), args)
	if err != nil {
		return nil, err
	}
	return toolResultJSON(t, result), nil
}

func newProjectRegistry(t *testing.T) (mcp.ToolRegistry, WriteDeps) {
	t.Helper()
	deps := newWriteDepsForTest(t)
	registry := mcp.ToolRegistry{}
	RegisterWriteTools(registry, deps)
	return registry, deps
}

func TestCreateProject_CreatesAndReturnsTheView(t *testing.T) {
	registry, deps := newProjectRegistry(t)

	out, err := invokeCreateProject(t, registry, map[string]any{
		"slug":         "diw-reviewapps",
		"name":         "DIW-ReviewApps",
		"description":  "Review apps for DIW",
		"color":        "#3b82f6",
		"setupCommand": "pnpm install",
	})
	require.NoError(t, err)

	require.Equal(t, "diw-reviewapps", out["slug"])
	require.Equal(t, "DIW-ReviewApps", out["name"])
	require.Equal(t, "Review apps for DIW", out["description"])
	require.Equal(t, "#3b82f6", out["color"])
	require.Equal(t, "pnpm install", out["setupCommand"])
	require.Equal(t, float64(0), out["folderCount"])
	require.NotEmpty(t, out["id"])

	stored, err := deps.ProjectRepo.GetBySlug(context.Background(), "diw-reviewapps")
	require.NoError(t, err)
	require.Equal(t, out["id"], stored.ID)
}

func TestCreateProject_OmittedOptionalsStayUnset(t *testing.T) {
	registry, _ := newProjectRegistry(t)

	out, err := invokeCreateProject(t, registry, map[string]any{
		"slug": "minimal",
		"name": "Minimal",
	})
	require.NoError(t, err)

	// omitempty on a nil pointer — an omitted optional must not persist as "".
	require.NotContains(t, out, "description")
	require.NotContains(t, out, "color")
	require.NotContains(t, out, "setupCommand")
	require.NotContains(t, out, "defaultSpawnerId")
}

func TestCreateProject_RejectsDuplicateSlug(t *testing.T) {
	registry, _ := newProjectRegistry(t)

	_, err := invokeCreateProject(t, registry, map[string]any{"slug": "web", "name": "Web"})
	require.NoError(t, err)

	_, err = invokeCreateProject(t, registry, map[string]any{"slug": "web", "name": "Web Again"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists")
}

func TestCreateProject_RejectsInvalidSlug(t *testing.T) {
	registry, _ := newProjectRegistry(t)

	// The exact input from the UI report: a name typed straight into the slug.
	_, err := invokeCreateProject(t, registry, map[string]any{"slug": "DIW-ReviewApps", "name": "DIW"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid slug")
}

func TestCreateProject_RejectsUnknownSpawner(t *testing.T) {
	registry, _ := newProjectRegistry(t)

	_, err := invokeCreateProject(t, registry, map[string]any{
		"slug":             "with-spawner",
		"name":             "With Spawner",
		"defaultSpawnerId": "no-such-spawner",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "spawner not found")
}

func TestCreateTask_ResolvesProjectSlug(t *testing.T) {
	registry, deps := newProjectRegistry(t)

	project, err := invokeCreateProject(t, registry, map[string]any{"slug": "web", "name": "Web"})
	require.NoError(t, err)

	_, err = invokeCreateTask(t, registry, map[string]any{
		"slug":        "add-login",
		"title":       "Add login",
		"cwd":         "/repos/web",
		"projectSlug": "web",
	})
	require.NoError(t, err)

	stored, err := deps.TaskRepo.GetBySlug(context.Background(), "add-login")
	require.NoError(t, err)
	require.NotNil(t, stored.ProjectID)
	require.Equal(t, project["id"], *stored.ProjectID)
}

func TestCreateTask_UnknownProjectSlugPointsAtCreateProject(t *testing.T) {
	registry, _ := newProjectRegistry(t)

	_, err := invokeCreateTask(t, registry, map[string]any{
		"slug":        "add-login",
		"title":       "Add login",
		"cwd":         "/repos/web",
		"projectSlug": "nope",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "project not found: nope")
	require.Contains(t, err.Error(), "create_project")
}

func TestCreateTask_RejectsBothProjectIdentifiers(t *testing.T) {
	registry, _ := newProjectRegistry(t)

	project, err := invokeCreateProject(t, registry, map[string]any{"slug": "web", "name": "Web"})
	require.NoError(t, err)

	_, err = invokeCreateTask(t, registry, map[string]any{
		"slug":        "add-login",
		"title":       "Add login",
		"cwd":         "/repos/web",
		"projectId":   project["id"],
		"projectSlug": "web",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not both")
}

func TestCreateProject_RequiresTasksWriteScope(t *testing.T) {
	require.Equal(t, "tasks:write", mcp.ToolScopeMap["create_project"])
}
