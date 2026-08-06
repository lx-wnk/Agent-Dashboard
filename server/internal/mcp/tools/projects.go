package tools

import (
	"context"
	"strings"

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
	// HasSetupCommand reports presence, never the text: a setup command
	// routinely embeds registry tokens and list_projects only costs tasks:read.
	HasSetupCommand bool   `json:"hasSetupCommand"`
	FolderCount     int    `json:"folderCount"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

// createdProjectView renders a freshly created project. A project has no folders
// until one is added through the UI or the folders API, so folderCount is zero
// by construction here rather than queried — do not reuse for a project loaded
// from the database.
func createdProjectView(p *ent.Project) projectView {
	return projectView{
		ID:               p.ID,
		Slug:             p.Slug,
		Name:             p.Name,
		Description:      p.Description,
		Color:            p.Color,
		DefaultSpawnerID: p.DefaultSpawnerID,
		HasSetupCommand:  p.SetupCommand != nil && *p.SetupCommand != "",
		FolderCount:      0,
		CreatedAt:        readTsFmt(p.CreatedAt),
		UpdatedAt:        readTsFmt(p.UpdatedAt),
	}
}

// registerCreateProject registers the create_project tool.
// Scope: tasks:write. Field rules mirror POST /api/projects — both writers must
// accept the same set of valid projects.
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
			name = strings.TrimSpace(name)
			if name == "" {
				return nil, mcp.Fail("create_project: name is required")
			}
			color, err := optionalPtr(args, "color")
			if err != nil {
				return nil, err
			}
			if color != nil && !validation.IsValidColor(*color) {
				return nil, mcp.Fail("create_project: " + validation.ColorPatternMessage)
			}
			if _, err := d.ProjectRepo.GetBySlug(ctx, slug); err == nil {
				return nil, mcp.Fail("create_project: project already exists: " + slug)
			} else if !ent.IsNotFound(err) {
				return nil, mcp.Fail("create_project: " + err.Error())
			}

			spawnerID, err := optionalPtr(args, "defaultSpawnerId")
			if err != nil {
				return nil, err
			}
			if spawnerID != nil {
				if d.SpawnerRepo == nil {
					return nil, mcp.Fail("create_project: spawner repository not configured")
				}
				if _, err := d.SpawnerRepo.GetByID(ctx, *spawnerID); err != nil {
					return nil, mcp.Fail("create_project: spawner not found")
				}
			}

			description, err := optionalPtr(args, "description")
			if err != nil {
				return nil, err
			}
			// setupCommand is deliberately absent from this tool: it is an
			// RCE-equivalent sink (`sh -c` in the worktree, see
			// serverapp/di_pipeline.go) that the HTTP writer gates behind an
			// admin check. tasks:write is not admin, so the MCP path always
			// creates a project without one — do not add it back for parity.
			p, err := d.ProjectRepo.Create(ctx, name, slug, description, color, spawnerID, nil)
			if err != nil {
				// Lost race against a concurrent create: the unique index, not the
				// pre-check above, is the authoritative duplicate guard.
				if ent.IsConstraintError(err) {
					return nil, mcp.Fail("create_project: project already exists: " + slug)
				}
				return nil, mcp.Fail("create_project: " + err.Error())
			}
			return mcp.OK(createdProjectView(p))
		},
	})
}

// optionalPtr returns a pointer to the named string argument, or nil when it is
// absent or blank — the repo layer distinguishes "leave unset" (nil) from "set
// to empty string", so an omitted optional must not become a pointer to "".
// Create-only: an update path needs the absent/null/value distinction instead
// (cf. api/projects.parseNullableString).
func optionalPtr(args map[string]any, key string) (*string, error) {
	raw, err := mcp.OptionalStringArg(args, key)
	if err != nil {
		return nil, err
	}
	if v := strings.TrimSpace(raw); v != "" {
		return &v, nil
	}
	return nil, nil
}
