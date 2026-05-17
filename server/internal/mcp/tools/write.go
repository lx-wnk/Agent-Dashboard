package tools

import (
	"context"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	mcp "github.com/lx-wnk/agent-dashboard/server/internal/mcp"
	"github.com/lx-wnk/agent-dashboard/server/internal/permissions"
	"github.com/lx-wnk/agent-dashboard/server/internal/validation"
)

// WriteDeps holds the repositories required by the write tools.
type WriteDeps struct {
	TaskRepo         repo.TaskRepo
	PermRepo         repo.PermissionRepo
	AuditRepo        repo.AuditRepo
	DepRepo          repo.DependencyRepo
	Broadcast        func(taskID string)
	BroadcastDeleted func(taskID string)
}

// permissionTemplates maps template names to their default tool grant lists.
var permissionTemplates = map[string][]repo.GrantEntry{
	"feature_implementation": {
		{Tool: "Read"}, {Tool: "Write"}, {Tool: "Edit"}, {Tool: "MultiEdit"},
		{Tool: "Glob"}, {Tool: "Grep"}, {Tool: "LS"}, {Tool: "Bash"}, {Tool: "WebFetch"},
	},
	"concept_baseline": {
		{Tool: "Read"}, {Tool: "Glob"}, {Tool: "Grep"}, {Tool: "WebFetch"}, {Tool: "WebSearch"},
	},
	"research_only": {
		{Tool: "Read"}, {Tool: "Glob"}, {Tool: "Grep"}, {Tool: "WebFetch"},
	},
	"test_only": {
		{Tool: "Read"}, {Tool: "Write"}, {Tool: "Edit"}, {Tool: "Glob"}, {Tool: "Grep"}, {Tool: "Bash"},
	},
	"review_only": {
		{Tool: "Read"}, {Tool: "Glob"}, {Tool: "Grep"},
	},
}

func init() {
	// Panic at startup if any template entry references a tool not in the allow-list,
	// so template drift is caught at build/test time rather than silently granted at runtime.
	for name, entries := range permissionTemplates {
		for _, e := range entries {
			if !permissions.IsAllowedTool(e.Tool) {
				panic("permissionTemplates[" + name + "]: unknown tool: " + e.Tool)
			}
		}
	}
}

// RegisterWriteTools registers all 6 write tools into the given registry.
func RegisterWriteTools(registry mcp.ToolRegistry, d WriteDeps) {
	registerCreateTask(registry, d)
	registerUpdateTask(registry, d)
	registerDeleteTask(registry, d)
	registerManageTask(registry, d)
	registerAddDependency(registry, d)
	registerRemoveDependency(registry, d)
}

// validateTool checks whether a tool name is in the pipeline allow-list.
func validateTool(tool string) bool {
	return permissions.IsAllowedTool(tool)
}

// applyTemplate bulk-grants all entries for a named template. Returns the number granted.
func applyTemplate(ctx context.Context, permRepo repo.PermissionRepo, taskID, templateName string) (int, error) {
	entries, ok := permissionTemplates[templateName]
	if !ok {
		return 0, mcp.Fail("unknown permission template: " + templateName)
	}
	granted, err := permRepo.BulkGrantPermissions(ctx, taskID, entries)
	if err != nil {
		return 0, err
	}
	return len(granted), nil
}

// permInputToGrantEntries converts the raw args permissions array to GrantEntry slice.
// Validates each tool against the allow-list; returns an mcp.Fail error on first violation.
type permissionInput struct {
	Tool      string  `json:"tool"`
	Pattern   *string `json:"pattern"`
	ExpiresAt *string `json:"expiresAt"`
}

func parsePermissionsArg(args map[string]any) ([]permissionInput, error) {
	raw, ok := args["permissions"]
	if !ok || raw == nil {
		return nil, nil
	}
	rawSlice, ok := raw.([]any)
	if !ok {
		return nil, mcp.Fail("permissions must be an array")
	}
	result := make([]permissionInput, 0, len(rawSlice))
	for _, item := range rawSlice {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, mcp.Fail("each permission entry must be an object")
		}
		tool, _ := m["tool"].(string)
		if tool == "" {
			return nil, mcp.Fail("permission entry missing tool")
		}
		var pattern *string
		if v, ok := m["pattern"].(string); ok {
			pattern = &v
		}
		var expiresAt *string
		if v, ok := m["expiresAt"].(string); ok {
			expiresAt = &v
		}
		result = append(result, permissionInput{Tool: tool, Pattern: pattern, ExpiresAt: expiresAt})
	}
	return result, nil
}

func permInputsToGrantEntries(perms []permissionInput) ([]repo.GrantEntry, error) {
	entries := make([]repo.GrantEntry, 0, len(perms))
	for _, p := range perms {
		if !validateTool(p.Tool) {
			return nil, mcp.Fail("tool not in allow-list: " + p.Tool)
		}
		entry := repo.GrantEntry{Tool: p.Tool}
		if p.Pattern != nil && *p.Pattern != "" {
			entry.Pattern = p.Pattern
		}
		if p.ExpiresAt != nil && *p.ExpiresAt != "" {
			t, err := time.Parse(time.RFC3339, *p.ExpiresAt)
			if err != nil {
				return nil, mcp.Fail("invalid expiresAt format (expected RFC3339): " + *p.ExpiresAt)
			}
			entry.ExpiresAt = &t
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// safeCall wraps Broadcast/BroadcastDeleted so nil funcs don't panic in tests.
func safeBroadcast(fn func(string), id string) {
	if fn != nil {
		fn(id)
	}
}

// registerCreateTask registers the create_task tool.
func registerCreateTask(registry mcp.ToolRegistry, d WriteDeps) {
	registry.Register(&mcp.ToolDef{
		Name:        "create_task",
		Description: "Create a new pipeline task.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"slug":               map[string]any{"type": "string", "description": "Unique slug matching ^[a-z0-9]+(?:-[a-z0-9]+)*$ (max 64 chars)"},
				"title":              map[string]any{"type": "string"},
				"cwd":                map[string]any{"type": "string", "description": "Absolute working directory path"},
				"description":        map[string]any{"type": "string"},
				"priority":           map[string]any{"type": "string", "enum": []string{"high", "medium", "low"}},
				"silverBullet":       map[string]any{"type": "boolean"},
				"metadata":           map[string]any{"type": "object"},
				"sourceBranch":       map[string]any{"type": "string"},
				"targetBranch":       map[string]any{"type": "string"},
				"parentTaskId":       map[string]any{"type": "string"},
				"maxIterations":      map[string]any{"type": "integer"},
				"tokenBudget":        map[string]any{"type": "integer"},
				"costBudgetCents":    map[string]any{"type": "integer"},
				"template":           map[string]any{"type": "string", "description": "Predefined permission template name"},
				"permissions":        map[string]any{"type": "array"},
				"inheritPermissions": map[string]any{"type": "boolean"},
			},
			"required": []string{"slug", "title", "cwd"},
		},
		Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
			slug, err := mcp.StringArg(args, "slug")
			if err != nil {
				return nil, err
			}
			if !validation.IsValidSlug(slug) {
				return nil, mcp.Fail("invalid slug: " + validation.SlugPatternMessage)
			}
			title, err := mcp.StringArg(args, "title")
			if err != nil {
				return nil, err
			}
			cwd, err := mcp.StringArg(args, "cwd")
			if err != nil {
				return nil, err
			}

			// Check slug uniqueness.
			existing, _ := d.TaskRepo.GetBySlug(ctx, slug)
			if existing != nil {
				return nil, mcp.Fail("slug already exists: " + slug)
			}

			// Pre-validate permissions before any DB writes.
			rawPerms, err := parsePermissionsArg(args)
			if err != nil {
				return nil, err
			}
			var grantEntries []repo.GrantEntry
			if len(rawPerms) > 0 {
				grantEntries, err = permInputsToGrantEntries(rawPerms)
				if err != nil {
					return nil, err
				}
			}

			// Build create input.
			in := repo.CreateTaskInput{
				Slug:         slug,
				Title:        title,
				Cwd:          cwd,
				CurrentStage: "concept",
				Priority:     "medium",
				SilverBullet: mcp.OptionalBool(args, "silverBullet"),
			}
			if v := mcp.OptionalString(args, "description"); v != "" {
				in.Description = &v
			}
			if v := mcp.OptionalString(args, "priority"); v != "" {
				in.Priority = v
			}
			if v := mcp.OptionalString(args, "sourceBranch"); v != "" {
				in.SourceBranch = &v
			}
			if v := mcp.OptionalString(args, "targetBranch"); v != "" {
				in.TargetBranch = &v
			}
			if v := mcp.OptionalString(args, "parentTaskId"); v != "" {
				in.ParentTaskID = &v
			}
			if f, ok := mcp.OptionalFloat64(args, "maxIterations"); ok {
				v := int(f)
				in.MaxIterations = v
			}
			if f, ok := mcp.OptionalFloat64(args, "tokenBudget"); ok {
				v := int(f)
				in.TokenBudget = &v
			}
			if f, ok := mcp.OptionalFloat64(args, "costBudgetCents"); ok {
				v := int(f)
				in.CostBudgetCents = &v
			}
			if rawMeta, ok := args["metadata"]; ok && rawMeta != nil {
				if m, ok := rawMeta.(map[string]any); ok {
					in.Metadata = m
				}
			}

			task, err := d.TaskRepo.Create(ctx, in)
			if err != nil {
				return nil, mcp.Fail("create_task: " + err.Error())
			}

			seedSummary := map[string]any{}

			// Apply template if provided.
			if templateName := mcp.OptionalString(args, "template"); templateName != "" {
				n, err := applyTemplate(ctx, d.PermRepo, task.ID, templateName)
				if err != nil {
					return nil, err
				}
				seedSummary["template"] = map[string]any{"name": templateName, "granted": n}
			}

			// Apply explicit permissions.
			if len(grantEntries) > 0 {
				granted, err := d.PermRepo.BulkGrantPermissions(ctx, task.ID, grantEntries)
				if err != nil {
					return nil, mcp.Fail("create_task permissions: " + err.Error())
				}
				seedSummary["explicit"] = map[string]any{"granted": len(granted)}
			}

			// Inherit from parent if requested and no explicit permissions given.
			parentTaskID := mcp.OptionalString(args, "parentTaskId")
			if mcp.OptionalBool(args, "inheritPermissions") && len(rawPerms) == 0 && parentTaskID != "" {
				inherited, err := d.PermRepo.InheritPermissionsFromParent(ctx, task.ID, parentTaskID)
				if err != nil {
					return nil, mcp.Fail("create_task inherit: " + err.Error())
				}
				seedSummary["inherited"] = map[string]any{"fromParent": parentTaskID, "granted": len(inherited)}
			}

			safeBroadcast(d.Broadcast, task.ID)
			return mcp.OK(map[string]any{"task": task, "permissions": seedSummary})
		},
	})
}

// registerUpdateTask registers the update_task tool.
func registerUpdateTask(registry mcp.ToolRegistry, d WriteDeps) {
	registry.Register(&mcp.ToolDef{
		Name:        "update_task",
		Description: "Update fields of an existing task.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":              map[string]any{"type": "string"},
				"title":           map[string]any{"type": "string"},
				"description":     map[string]any{"type": "string"},
				"priority":        map[string]any{"type": "string", "enum": []string{"high", "medium", "low"}},
				"silverBullet":    map[string]any{"type": "boolean"},
				"maxIterations":   map[string]any{"type": "integer"},
				"tokenBudget":     map[string]any{"type": "integer"},
				"costBudgetCents": map[string]any{"type": "integer"},
				"metadata":        map[string]any{"type": "object"},
			},
			"required": []string{"id"},
		},
		Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
			id, err := mcp.StringArg(args, "id")
			if err != nil {
				return nil, err
			}
			task, err := d.TaskRepo.GetByID(ctx, id)
			if err != nil {
				return nil, mcp.Fail("Task not found: " + id)
			}

			in := repo.UpdateTaskInput{}
			if v := mcp.OptionalString(args, "title"); v != "" {
				in.Title = &v
			}
			if _, hasDesc := args["description"]; hasDesc {
				v := mcp.OptionalString(args, "description")
				in.Description = &v
			}
			if v := mcp.OptionalString(args, "priority"); v != "" {
				in.Priority = &v
			}
			if _, hasSB := args["silverBullet"]; hasSB {
				v := mcp.OptionalBool(args, "silverBullet")
				in.SilverBullet = &v
			}
			if f, ok := mcp.OptionalFloat64(args, "maxIterations"); ok {
				v := int(f)
				in.MaxIterations = &v
			}
			if f, ok := mcp.OptionalFloat64(args, "tokenBudget"); ok {
				v := int(f)
				in.TokenBudget = &v
			}
			if f, ok := mcp.OptionalFloat64(args, "costBudgetCents"); ok {
				v := int(f)
				in.CostBudgetCents = &v
			}
			if rawMeta, ok := args["metadata"]; ok && rawMeta != nil {
				if m, ok := rawMeta.(map[string]any); ok {
					in.Metadata = m
				}
			}

			updated, err := d.TaskRepo.Update(ctx, task.ID, in)
			if err != nil {
				return nil, mcp.Fail("update_task: " + err.Error())
			}
			safeBroadcast(d.Broadcast, updated.ID)
			return mcp.OK(updated)
		},
	})
}

// registerDeleteTask registers the delete_task tool.
func registerDeleteTask(registry mcp.ToolRegistry, d WriteDeps) {
	registry.Register(&mcp.ToolDef{
		Name:        "delete_task",
		Description: "Permanently delete a task by ID.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "string"},
			},
			"required": []string{"id"},
		},
		Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
			id, err := mcp.StringArg(args, "id")
			if err != nil {
				return nil, err
			}
			_, err = d.TaskRepo.GetByID(ctx, id)
			if err != nil {
				return nil, mcp.Fail("Task not found: " + id)
			}
			if err := d.TaskRepo.Delete(ctx, id); err != nil {
				return nil, mcp.Fail("delete_task: " + err.Error())
			}
			safeBroadcast(d.BroadcastDeleted, id)
			return mcp.OK(map[string]bool{"success": true})
		},
	})
}

// registerManageTask registers the consolidated manage_task tool.
func registerManageTask(registry mcp.ToolRegistry, d WriteDeps) {
	registry.Register(&mcp.ToolDef{
		Name: "manage_task",
		Description: "Retroactive task management: grant/revoke/list permissions, inherit from parent, " +
			"or update metadata, priority, or budgets.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{"type": "string"},
				"action": map[string]any{
					"type": "string",
					"enum": []string{
						"grant_permissions", "revoke_permission", "list_permissions",
						"inherit_from_parent", "set_metadata", "set_priority", "set_budget",
					},
				},
				"template":       map[string]any{"type": "string"},
				"permissions":    map[string]any{"type": "array"},
				"permission_id":  map[string]any{"type": "string"},
				"effective_only": map[string]any{"type": "boolean"},
				"metadata_patch": map[string]any{"type": "object"},
				"priority":       map[string]any{"type": "string", "enum": []string{"high", "medium", "low"}},
				"silverBullet":   map[string]any{"type": "boolean"},
				"tokenBudget":    map[string]any{"type": "integer"},
				"costBudgetCents": map[string]any{"type": "integer"},
				"maxIterations":  map[string]any{"type": "integer"},
			},
			"required": []string{"task_id", "action"},
		},
		Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
			taskID, err := mcp.StringArg(args, "task_id")
			if err != nil {
				return nil, err
			}
			action, err := mcp.StringArg(args, "action")
			if err != nil {
				return nil, err
			}

			task, err := d.TaskRepo.GetByID(ctx, taskID)
			if err != nil {
				return nil, mcp.Fail("Task not found: " + taskID)
			}

			switch action {
			case "grant_permissions":
				return handleGrantPermissions(ctx, d, taskID, args)
			case "revoke_permission":
				return handleRevokePermission(ctx, d, taskID, args)
			case "list_permissions":
				return handleListPermissions(ctx, d, taskID, args)
			case "inherit_from_parent":
				return handleInheritFromParent(ctx, d, task, args)
			case "set_metadata":
				return handleSetMetadata(ctx, d, task, args)
			case "set_priority":
				return handleSetPriority(ctx, d, task, args)
			case "set_budget":
				return handleSetBudget(ctx, d, task, args)
			default:
				return nil, mcp.Fail("unknown action: " + action)
			}
		},
	})
}

func handleGrantPermissions(ctx context.Context, d WriteDeps, taskID string, args map[string]any) (*mcp.ToolResult, error) {
	rawPerms, err := parsePermissionsArg(args)
	if err != nil {
		return nil, err
	}
	summary := map[string]any{}

	if templateName := mcp.OptionalString(args, "template"); templateName != "" {
		n, err := applyTemplate(ctx, d.PermRepo, taskID, templateName)
		if err != nil {
			return nil, err
		}
		summary["template"] = map[string]any{"name": templateName, "granted": n}
	}
	if len(rawPerms) > 0 {
		entries, err := permInputsToGrantEntries(rawPerms)
		if err != nil {
			return nil, err
		}
		granted, err := d.PermRepo.BulkGrantPermissions(ctx, taskID, entries)
		if err != nil {
			return nil, mcp.Fail("grant_permissions: " + err.Error())
		}
		summary["explicit"] = map[string]any{"granted": len(granted)}
	}
	// Audit is best-effort: a failed append must not block the user-visible operation.
	_ = d.AuditRepo.Append(ctx, repo.AppendAuditInput{
		TaskID: taskID, Actor: "system", Action: "permissions_granted",
		Details: map[string]any{"summary": summary, "source": "mcp_manage_task"},
	})
	safeBroadcast(d.Broadcast, taskID)
	return mcp.OK(map[string]any{"action": "grant_permissions", "summary": summary})
}

func handleRevokePermission(ctx context.Context, d WriteDeps, taskID string, args map[string]any) (*mcp.ToolResult, error) {
	permID := mcp.OptionalString(args, "permission_id")
	if permID == "" {
		return nil, mcp.Fail("permission_id is required for revoke_permission")
	}
	if err := d.PermRepo.DeleteTaskPermission(ctx, permID); err != nil {
		return nil, mcp.Fail("revoke_permission: " + err.Error())
	}
	// Audit is best-effort: a failed append must not block the user-visible operation.
	_ = d.AuditRepo.Append(ctx, repo.AppendAuditInput{
		TaskID: taskID, Actor: "system", Action: "permission_revoked",
		Details: map[string]any{"permissionId": permID, "source": "mcp_manage_task"},
	})
	safeBroadcast(d.Broadcast, taskID)
	return mcp.OK(map[string]any{"action": "revoke_permission", "removed": permID})
}

func handleListPermissions(ctx context.Context, d WriteDeps, taskID string, args map[string]any) (*mcp.ToolResult, error) {
	effectiveOnly := true // default true per TypeScript reference
	if _, hasFlag := args["effective_only"]; hasFlag {
		effectiveOnly = mcp.OptionalBool(args, "effective_only")
	}
	var perms any
	var err error
	if effectiveOnly {
		perms, err = d.PermRepo.ListEffectiveTaskPermissions(ctx, taskID)
	} else {
		perms, err = d.PermRepo.ListTaskPermissions(ctx, taskID)
	}
	if err != nil {
		return nil, mcp.Fail("list_permissions: " + err.Error())
	}
	return mcp.OK(map[string]any{"action": "list_permissions", "permissions": perms})
}

func handleInheritFromParent(ctx context.Context, d WriteDeps, task *ent.Task, args map[string]any) (*mcp.ToolResult, error) {
	taskID := task.ID
	if task.ParentTaskID == nil || *task.ParentTaskID == "" {
		return nil, mcp.Fail("Task has no parentTaskId — cannot inherit")
	}
	inherited, err := d.PermRepo.InheritPermissionsFromParent(ctx, taskID, *task.ParentTaskID)
	if err != nil {
		return nil, mcp.Fail("inherit_from_parent: " + err.Error())
	}
	// Audit is best-effort: a failed append must not block the user-visible operation.
	_ = d.AuditRepo.Append(ctx, repo.AppendAuditInput{
		TaskID: taskID, Actor: "system", Action: "permissions_inherited",
		Details: map[string]any{"fromParent": *task.ParentTaskID, "granted": len(inherited), "source": "mcp_manage_task"},
	})
	safeBroadcast(d.Broadcast, taskID)
	return mcp.OK(map[string]any{
		"action": "inherit_from_parent", "from": *task.ParentTaskID, "granted": len(inherited),
	})
}

// metadataAllowList is the set of metadata keys MCP callers may set.
// Security-sensitive keys (e.g. allowGitPush) are intentionally excluded
// so they cannot be escalated via the MCP endpoint.
var metadataAllowList = map[string]bool{
	"description": true,
	"tags":        true,
	"externalId":  true,
	"notes":       true,
	"category":    true,
	"source":      true,
}

func handleSetMetadata(ctx context.Context, d WriteDeps, task *ent.Task, args map[string]any) (*mcp.ToolResult, error) {
	taskID := task.ID
	rawPatch, ok := args["metadata_patch"]
	if !ok || rawPatch == nil {
		return nil, mcp.Fail("metadata_patch with at least one key is required")
	}
	patch, ok := rawPatch.(map[string]any)
	if !ok || len(patch) == 0 {
		return nil, mcp.Fail("metadata_patch with at least one key is required")
	}
	for k := range patch {
		if !metadataAllowList[k] {
			return nil, mcp.Fail("metadata key not allowed: " + k)
		}
	}
	existing := map[string]any{}
	if task.Metadata != nil {
		existing = task.Metadata
	}
	merged := make(map[string]any, len(existing)+len(patch))
	for k, v := range existing {
		merged[k] = v
	}
	for k, v := range patch {
		merged[k] = v
	}
	updated, err := d.TaskRepo.Update(ctx, taskID, repo.UpdateTaskInput{Metadata: merged})
	if err != nil {
		return nil, mcp.Fail("set_metadata: " + err.Error())
	}
	// Audit is best-effort: a failed append must not block the user-visible operation.
	_ = d.AuditRepo.Append(ctx, repo.AppendAuditInput{
		TaskID: taskID, Actor: "system", Action: "metadata_patched",
		Details: map[string]any{"keys": mapKeys(patch), "source": "mcp_manage_task"},
	})
	safeBroadcast(d.Broadcast, taskID)
	return mcp.OK(map[string]any{"action": "set_metadata", "task": updated})
}

func handleSetPriority(ctx context.Context, d WriteDeps, task *ent.Task, args map[string]any) (*mcp.ToolResult, error) {
	taskID := task.ID
	prio := mcp.OptionalString(args, "priority")
	_, hasSB := args["silverBullet"]
	if prio == "" && !hasSB {
		return nil, mcp.Fail("priority or silverBullet must be provided")
	}
	in := repo.UpdateTaskInput{}
	if prio != "" {
		in.Priority = &prio
	}
	if hasSB {
		v := mcp.OptionalBool(args, "silverBullet")
		in.SilverBullet = &v
	}
	updated, err := d.TaskRepo.Update(ctx, taskID, in)
	if err != nil {
		return nil, mcp.Fail("set_priority: " + err.Error())
	}
	details := map[string]any{"source": "mcp_manage_task"}
	if prio != "" {
		details["priority"] = prio
	}
	if hasSB {
		details["silverBullet"] = mcp.OptionalBool(args, "silverBullet")
	}
	// Audit is best-effort: a failed append must not block the user-visible operation.
	_ = d.AuditRepo.Append(ctx, repo.AppendAuditInput{
		TaskID: taskID, Actor: "system", Action: "priority_changed", Details: details,
	})
	safeBroadcast(d.Broadcast, taskID)
	return mcp.OK(map[string]any{"action": "set_priority", "task": updated})
}

func handleSetBudget(ctx context.Context, d WriteDeps, task *ent.Task, args map[string]any) (*mcp.ToolResult, error) {
	taskID := task.ID
	in := repo.UpdateTaskInput{}
	anySet := false
	if f, ok := mcp.OptionalFloat64(args, "tokenBudget"); ok {
		v := int(f)
		in.TokenBudget = &v
		anySet = true
	}
	if f, ok := mcp.OptionalFloat64(args, "costBudgetCents"); ok {
		v := int(f)
		in.CostBudgetCents = &v
		anySet = true
	}
	if f, ok := mcp.OptionalFloat64(args, "maxIterations"); ok {
		v := int(f)
		in.MaxIterations = &v
		anySet = true
	}
	if !anySet {
		return nil, mcp.Fail("at least one of tokenBudget, costBudgetCents, maxIterations is required")
	}
	updated, err := d.TaskRepo.Update(ctx, taskID, in)
	if err != nil {
		return nil, mcp.Fail("set_budget: " + err.Error())
	}
	// Audit is best-effort: a failed append must not block the user-visible operation.
	_ = d.AuditRepo.Append(ctx, repo.AppendAuditInput{
		TaskID: taskID, Actor: "system", Action: "budget_changed",
		Details: map[string]any{"source": "mcp_manage_task"},
	})
	safeBroadcast(d.Broadcast, taskID)
	return mcp.OK(map[string]any{"action": "set_budget", "task": updated})
}

// registerAddDependency registers the add_dependency tool.
func registerAddDependency(registry mcp.ToolRegistry, d WriteDeps) {
	registry.Register(&mcp.ToolDef{
		Name:        "add_dependency",
		Description: "Add a dependency between two tasks (task_id waits for depends_on_id).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id":          map[string]any{"type": "string", "description": "ID of the dependent task (the one that waits)"},
				"depends_on_id":    map[string]any{"type": "string", "description": "ID of the prerequisite task"},
				"required_stage":   map[string]any{"type": "string", "enum": []string{"done", "cancelled"}, "description": "Stage the prerequisite must reach (default: done)"},
				"on_cancel_action": map[string]any{"type": "string", "enum": []string{"cancel", "start", "on_hold"}, "description": "What to do when prerequisite is cancelled (default: on_hold)"},
			},
			"required": []string{"task_id", "depends_on_id"},
		},
		Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
			taskID, err := mcp.StringArg(args, "task_id")
			if err != nil {
				return nil, err
			}
			dependsOnID, err := mcp.StringArg(args, "depends_on_id")
			if err != nil {
				return nil, err
			}

			if _, err := d.TaskRepo.GetByID(ctx, taskID); err != nil {
				return nil, mcp.Fail("Task not found: " + taskID)
			}
			if _, err := d.TaskRepo.GetByID(ctx, dependsOnID); err != nil {
				return nil, mcp.Fail("Prerequisite task not found: " + dependsOnID)
			}

			requiredStage := mcp.OptionalString(args, "required_stage")
			if requiredStage == "" {
				requiredStage = "done"
			}
			onCancelAction := mcp.OptionalString(args, "on_cancel_action")
			if onCancelAction == "" {
				onCancelAction = "on_hold"
			}

			dep, err := d.DepRepo.Add(ctx, taskID, dependsOnID, requiredStage, onCancelAction)
			if err != nil {
				if ent.IsConstraintError(err) {
					return nil, mcp.Fail("Dependency already exists")
				}
				return nil, mcp.Fail("add_dependency: " + err.Error())
			}
			safeBroadcast(d.Broadcast, taskID)
			return mcp.OK(dep)
		},
	})
}

// registerRemoveDependency registers the remove_dependency tool.
func registerRemoveDependency(registry mcp.ToolRegistry, d WriteDeps) {
	registry.Register(&mcp.ToolDef{
		Name:        "remove_dependency",
		Description: "Remove a dependency between two tasks.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id":       map[string]any{"type": "string", "description": "ID of the dependent task"},
				"depends_on_id": map[string]any{"type": "string", "description": "ID of the prerequisite task to remove"},
			},
			"required": []string{"task_id", "depends_on_id"},
		},
		Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
			taskID, err := mcp.StringArg(args, "task_id")
			if err != nil {
				return nil, err
			}
			dependsOnID, err := mcp.StringArg(args, "depends_on_id")
			if err != nil {
				return nil, err
			}

			removed, err := d.DepRepo.Remove(ctx, taskID, dependsOnID)
			if err != nil {
				return nil, mcp.Fail("remove_dependency: " + err.Error())
			}
			if removed {
				safeBroadcast(d.Broadcast, taskID)
			}
			return mcp.OK(map[string]bool{"removed": removed})
		},
	})
}

// mapKeys returns the keys of a map[string]any as a []string.
func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
