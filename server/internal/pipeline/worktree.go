package pipeline

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
)

// ensureTaskWorktree creates a git worktree for task at <worktreeRoot>/<slug>.
// Called before spawning an agent when SourceBranch is set but WorktreePath is empty.
// Idempotent: returns the path immediately if the directory already exists.
func ensureTaskWorktree(task *ent.Task, worktreeRoot string) (string, error) {
	if task.SourceBranch == nil || *task.SourceBranch == "" {
		return "", fmt.Errorf("ensureTaskWorktree: task has no source_branch")
	}
	if worktreeRoot == "" {
		home, _ := os.UserHomeDir()
		worktreeRoot = filepath.Join(home, ".claude", "dashboard-worktrees")
	}
	worktreePath := filepath.Join(worktreeRoot, task.Slug)

	// Already exists — assume a prior run set it up correctly.
	if _, err := os.Stat(worktreePath); err == nil {
		return worktreePath, nil
	}

	if err := os.MkdirAll(worktreeRoot, 0o750); err != nil {
		return "", fmt.Errorf("ensureTaskWorktree: mkdir %s: %w", worktreeRoot, err)
	}

	branch := *task.SourceBranch

	// Try creating a new branch + worktree from HEAD.
	out, err := exec.Command("git", "-C", task.Cwd, "worktree", "add", "-b", branch, worktreePath).CombinedOutput()
	if err != nil {
		// Branch already exists — check it out in the worktree without -b.
		out2, err2 := exec.Command("git", "-C", task.Cwd, "worktree", "add", worktreePath, branch).CombinedOutput()
		if err2 != nil {
			return "", fmt.Errorf("ensureTaskWorktree: git worktree add failed: %s / %s",
				strings.TrimSpace(string(out)), strings.TrimSpace(string(out2)))
		}
	}
	return worktreePath, nil
}

// removeTaskWorktree runs `git worktree remove --force` for the given path.
// Non-fatal by design — callers log the error and continue.
func removeTaskWorktree(cwd, worktreePath string) error {
	if worktreePath == "" {
		return nil
	}
	out, err := exec.Command("git", "-C", cwd, "worktree", "remove", "--force", worktreePath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("removeTaskWorktree: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}
