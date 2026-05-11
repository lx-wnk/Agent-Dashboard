package pipeline

import (
	"context"
	"fmt"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// conceptStageTools is the minimum tool set the concept-stage agent needs to explore a codebase.
var conceptStageTools = []string{"Read", "Glob", "Grep"}

// SaveGrantsToPresets persists all current effective task permissions as presets for the
// task's cwd + userID combination. Call after refine confirm so future tasks in the same
// project inherit the same permissions.
func SaveGrantsToPresets(
	ctx context.Context,
	taskID string,
	userID *string,
	cwd string,
	permRepo repo.PermissionRepo,
	presetRepo repo.PermissionPresetRepo,
) error {
	perms, err := permRepo.ListEffectiveTaskPermissions(ctx, taskID)
	if err != nil {
		return fmt.Errorf("SaveGrantsToPresets: list permissions: %w", err)
	}
	for _, p := range perms {
		if err := presetRepo.Upsert(ctx, repo.UpsertPresetInput{
			UserID:     userID,
			ProjectCwd: cwd,
			Tool:       p.Tool,
			Pattern:    p.Pattern,
		}); err != nil {
			return fmt.Errorf("SaveGrantsToPresets: upsert preset: %w", err)
		}
	}
	return nil
}

// ApplyPresetPermissions loads saved presets for a project (userID + cwd) and grants any
// that are not already present on the task.
func ApplyPresetPermissions(
	ctx context.Context,
	taskID string,
	userID *string,
	cwd string,
	permRepo repo.PermissionRepo,
	presetRepo repo.PermissionPresetRepo,
) error {
	summaries, err := presetRepo.ListSummaries(ctx, userID)
	if err != nil {
		return fmt.Errorf("ApplyPresetPermissions: list summaries: %w", err)
	}

	// Find presets for this project.
	var presetEntries []repo.PresetEntry
	for _, s := range summaries {
		if s.ProjectCwd == cwd {
			presetEntries = s.Entries
			break
		}
	}
	if len(presetEntries) == 0 {
		return nil
	}

	// Fetch existing task permissions to avoid duplicates.
	existing, err := permRepo.ListEffectiveTaskPermissions(ctx, taskID)
	if err != nil {
		return fmt.Errorf("ApplyPresetPermissions: list existing permissions: %w", err)
	}
	type key struct{ tool, pattern string }
	has := make(map[key]bool, len(existing))
	for _, p := range existing {
		pat := ""
		if p.Pattern != nil {
			pat = *p.Pattern
		}
		has[key{p.Tool, pat}] = true
	}

	// Grant missing entries.
	for _, entry := range presetEntries {
		pat := ""
		if entry.Pattern != nil {
			pat = *entry.Pattern
		}
		if has[key{entry.Tool, pat}] {
			continue
		}
		if _, err := permRepo.BulkGrantPermissions(ctx, taskID, []repo.GrantEntry{
			{Tool: entry.Tool, Pattern: entry.Pattern},
		}); err != nil {
			return fmt.Errorf("ApplyPresetPermissions: grant %s: %w", entry.Tool, err)
		}
	}
	return nil
}

// BulkGrantConceptStagePermissions grants Read/Glob/Grep to a task when they are not already
// present. These are the minimum tools the concept-stage agent needs to explore the codebase.
func BulkGrantConceptStagePermissions(
	ctx context.Context,
	taskID string,
	permRepo repo.PermissionRepo,
) error {
	existing, err := permRepo.ListEffectiveTaskPermissions(ctx, taskID)
	if err != nil {
		return fmt.Errorf("BulkGrantConceptStagePermissions: list existing: %w", err)
	}
	has := make(map[string]bool, len(existing))
	for _, p := range existing {
		has[p.Tool] = true
	}

	var entries []repo.GrantEntry
	for _, tool := range conceptStageTools {
		if !has[tool] {
			entries = append(entries, repo.GrantEntry{Tool: tool})
		}
	}
	if len(entries) == 0 {
		return nil
	}
	if _, err := permRepo.BulkGrantPermissions(ctx, taskID, entries); err != nil {
		return fmt.Errorf("BulkGrantConceptStagePermissions: %w", err)
	}
	return nil
}
