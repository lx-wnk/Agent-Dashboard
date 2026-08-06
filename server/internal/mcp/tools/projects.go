package tools

import (
	"context"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	mcp "github.com/lx-wnk/agent-dashboard/server/internal/mcp"
	"github.com/lx-wnk/agent-dashboard/server/internal/validation"
)

// projectView is the JSON shape returned by the list_projects and
// create_project MCP tools. Mirrors the camelCase wire shape used by the
// projects HTTP handler.
type projectView struct {
	ID               string  `json:"id"`
	Slug             string  `json:"slug"`
	Name             string  `json:"name"`
	Description      *string `json:"description,omitempty"`
	Color            *string `json:"color,omitempty"`
	DefaultSpawnerID *string `json:"defaultSpawnerId,omitempty"`
	SetupCommand     *string `json:"setupCommand,omitempty"`
	FolderCount      int     `json:"folderCount"`
	CreatedAt        string  `json:"createdAt"`
	UpdatedAt        string  `json:"updatedAt"`
}

// newProjectView renders a freshly created project. A project has no folders
// until one is added through the UI or the folders API, so folderCount is zero
// by construction here rather than queried.
func newProjectView(p *ent.Project) projectView {
	return projectView{
		ID:               p.ID,
		Slug:             p.Slug,
		Name:             p.Name,
		Description:      p.Description,
		Color:            p.Color,
		DefaultSpawnerID: p.DefaultSpawnerID,
		SetupCommand:     p.SetupCommand,
		FolderCount:      0,
		CreatedAt:        readTsFmt(p.CreatedAt),
		UpdatedAt:        readTsFmt(p.UpdatedAt),
	}
}

// registerCreateProject registers the create_project tool, so an agent that
// finds no matching project in list_projects can create one instead of needing
// a human to open the settings UI.
// Scope: tasks:write.
func registerCreateProject(registry mcp.ToolRegistry, d WriteDeps) {
	registry.Register(&mcp.ToolDef{
		Name:        "create_project",
		Description: "Create a project. Fails if the slug is already taken — call list_projects first to reuse an existing project.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"slug":             map[string]any{"type": "string", "description": "Unique " + validation.SlugPatternMessage},
				"name":             map[string]any{"type": "string", "description": "Human-readable project name"},
				"description":      map[string]any{"type": "string"},
				"color":            map[string]any{"type": "string", "description": "Hex colour for the dashboard, e.g. #3b82f6"},
				"defaultSpawnerId": map[string]any{"type": "string", "description": "Optional spawner ID used by this project's tasks by default"},
				"setupCommand":     map[string]any{"type": "string", "description": "Run once in the worktree after it is created"},
			},
			"required": []string{"slug", "name"},
		},
		Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
			if d.ProjectRepo == nil {
				return nil, mcp.Fail("create_project: project repository not configured")
			}
			slug, err := mcp.StringArg(args, "slug")
			if err != nil {
				return nil, err
			}
			if !validation.IsValidSlug(slug) {
				return nil, mcp.Fail("invalid slug: " + validation.SlugPatternMessage)
			}
			name, err := mcp.StringArg(args, "name")
			if err != nil {
				return nil, err
			}
			if _, err := d.ProjectRepo.GetBySlug(ctx, slug); err == nil {
				return nil, mcp.Fail("create_project: project already exists: " + slug)
			}

			spawnerID := optionalPtr(args, "defaultSpawnerId")
			if spawnerID != nil {
				if d.SpawnerRepo == nil {
					return nil, mcp.Fail("create_project: spawner repository not configured")
				}
				if _, err := d.SpawnerRepo.GetByID(ctx, *spawnerID); err != nil {
					return nil, mcp.Fail("create_project: spawner not found")
				}
			}

			p, err := d.ProjectRepo.Create(
				ctx,
				name,
				slug,
				optionalPtr(args, "description"),
				optionalPtr(args, "color"),
				spawnerID,
				optionalPtr(args, "setupCommand"),
			)
			if err != nil {
				return nil, mcp.Fail("create_project: " + err.Error())
			}
			return mcp.OK(newProjectView(p))
		},
	})
}

// optionalPtr returns a pointer to the named string argument, or nil when it is
// absent or empty — the repo layer distinguishes "leave unset" (nil) from "set
// to empty string", so an omitted optional must not become a pointer to "".
func optionalPtr(args map[string]any, key string) *string {
	if v := mcp.OptionalString(args, key); v != "" {
		return &v
	}
	return nil
}
