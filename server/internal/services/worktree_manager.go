package services

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/worktree"
)

// WorktreeManager exposes read-only status queries about a task's git worktree.
// Mutating worktree operations live in package pipeline (see
// `pipeline/worktree.go`); this service is consumed by the HTTP/MCP layers.
type WorktreeManager struct {
	taskRepo repo.TaskRepo
	runner   *worktree.Runner
}

// NewWorktreeManager returns a WorktreeManager backed by the given task repo.
func NewWorktreeManager(taskRepo repo.TaskRepo) *WorktreeManager {
	return &WorktreeManager{
		taskRepo: taskRepo,
		runner:   worktree.NewRunner(),
	}
}

// WorktreeStatus returns the worktree status for the given task: branch, ahead/
// behind counts vs the task's base branch on `origin`, dirty flag, and dirty
// file count.
//
// Behaviour notes:
//   - When the task has no worktree, returns (nil, nil) — the caller renders
//     "no worktree" gracefully.
//   - When `origin/<base>` cannot be resolved (e.g. the base branch is local-
//     only), Ahead and Behind are returned as nil pointers — never an error.
//   - Any underlying git error other than "task not found" is swallowed and
//     surfaces as zeroed counts; the goal is best-effort UI enrichment.
func (m *WorktreeManager) WorktreeStatus(ctx context.Context, taskID string) (*sdk.WorktreeStatusDTO, error) {
	task, err := m.taskRepo.GetByID(ctx, taskID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("worktree_status: task %s: %w", taskID, err)
		}
		return nil, fmt.Errorf("worktree_status: get task: %w", err)
	}

	if task.WorktreePath == nil || *task.WorktreePath == "" {
		return nil, nil
	}
	cwd := *task.WorktreePath

	branch := m.currentBranch(ctx, cwd)
	if branch == "" && task.SourceBranch != nil {
		branch = *task.SourceBranch
	}

	base := ""
	if task.TargetBranch != nil && *task.TargetBranch != "" {
		base = *task.TargetBranch
	} else if task.SourceBranch != nil && *task.SourceBranch != "" {
		base = *task.SourceBranch
	}

	dirty, fileCount := m.dirtyState(ctx, cwd)

	dto := &sdk.WorktreeStatusDTO{
		Branch:    branch,
		Dirty:     dirty,
		FileCount: fileCount,
	}

	if base != "" && branch != "" && m.remoteRefExists(ctx, cwd, base) {
		ahead := m.revListCount(ctx, cwd, "origin/"+base+".."+branch)
		behind := m.revListCount(ctx, cwd, branch+"..origin/"+base)
		if ahead != nil {
			dto.Ahead = ahead
		}
		if behind != nil {
			dto.Behind = behind
		}
	}

	return dto, nil
}

func (m *WorktreeManager) currentBranch(ctx context.Context, cwd string) string {
	out, err := m.runner.Output(ctx, cwd, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func (m *WorktreeManager) remoteRefExists(ctx context.Context, cwd, base string) bool {
	_, err := m.runner.Output(ctx, cwd, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+base)
	return err == nil
}

// revListCount returns the integer count from `git rev-list --count <range>`,
// or nil if the command failed (e.g. ambiguous ref). Pointer return lets the
// API expose JSON null when the count is genuinely unknown.
func (m *WorktreeManager) revListCount(ctx context.Context, cwd, rng string) *int {
	out, err := m.runner.Output(ctx, cwd, "rev-list", "--count", rng)
	if err != nil {
		return nil
	}
	trimmed := strings.TrimSpace(out)
	n, err := strconv.Atoi(trimmed)
	if err != nil {
		return nil
	}
	return &n
}

func (m *WorktreeManager) dirtyState(ctx context.Context, cwd string) (bool, int) {
	out, err := m.runner.Output(ctx, cwd, "status", "--porcelain")
	if err != nil {
		return false, 0
	}
	trimmed := strings.TrimRight(out, "\n")
	if trimmed == "" {
		return false, 0
	}
	count := strings.Count(trimmed, "\n") + 1
	return true, count
}
