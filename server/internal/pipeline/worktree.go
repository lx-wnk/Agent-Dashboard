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
// Idempotent: returns the path immediately if the directory already exists.
// When task.SourceBranch is nil, falls back to "feat/<slug>" as the branch name.
// Returns the worktree path and the branch name that was used.
func ensureTaskWorktree(task *ent.Task, worktreeRoot string) (path, branch string, err error) {
	if worktreeRoot == "" {
		home, _ := os.UserHomeDir()
		worktreeRoot = filepath.Join(home, "dashboard-worktrees")
	}
	worktreePath := filepath.Join(worktreeRoot, task.Slug)

	if task.SourceBranch != nil && *task.SourceBranch != "" {
		branch = *task.SourceBranch
	} else {
		branch = "feat/" + task.Slug
	}

	// Already exists — assume a prior run set it up correctly.
	if _, err := os.Stat(worktreePath); err == nil {
		return worktreePath, branch, nil
	}

	if err := os.MkdirAll(worktreeRoot, 0o750); err != nil {
		return "", "", fmt.Errorf("ensureTaskWorktree: mkdir %s: %w", worktreeRoot, err)
	}

	// Try creating a new branch + worktree from HEAD.
	out, cmdErr := exec.Command("git", "-C", task.Cwd, "worktree", "add", "-b", branch, worktreePath).CombinedOutput()
	if cmdErr != nil {
		// Branch already exists — check it out in the worktree without -b.
		out2, err2 := exec.Command("git", "-C", task.Cwd, "worktree", "add", worktreePath, branch).CombinedOutput()
		if err2 != nil {
			return "", "", fmt.Errorf("ensureTaskWorktree: git worktree add failed: %s / %s",
				strings.TrimSpace(string(out)), strings.TrimSpace(string(out2)))
		}
	}
	return worktreePath, branch, nil
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
