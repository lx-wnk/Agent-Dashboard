package pipeline

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/worktree"
)

// gitRunner runs git for worktree mutations with a bounded timeout.
var gitRunner = worktree.NewRunner()

// ensureTaskWorktree creates a git worktree for task at <worktreeRoot>/<slug>.
// Idempotent: returns the path immediately if the directory already exists.
// When task.SourceBranch is nil, falls back to "feat/<slug>" as the branch name.
// Returns the worktree path and the branch name that was used.
func ensureTaskWorktree(task *ent.Task, worktreeRoot string) (path, branch string, err error) {
	worktreeRoot = worktree.DefaultRoot(worktreeRoot)
	worktreePath := worktree.PathFor(worktreeRoot, task.Slug)
	branch = worktree.CreateBranch(task.SourceBranch, task.Slug)

	// Already exists — assume a prior run set it up correctly.
	if _, err := os.Stat(worktreePath); err == nil {
		return worktreePath, branch, nil
	}

	if err := os.MkdirAll(worktreeRoot, 0o750); err != nil {
		return "", "", fmt.Errorf("ensureTaskWorktree: mkdir %s: %w", worktreeRoot, err)
	}

	ctx := context.Background()
	// Try creating a new branch + worktree from HEAD.
	out, cmdErr := gitRunner.Combined(ctx, task.Cwd, "worktree", "add", "-b", branch, worktreePath)
	if cmdErr != nil {
		// Branch already exists — check it out in the worktree without -b.
		out2, err2 := gitRunner.Combined(ctx, task.Cwd, "worktree", "add", worktreePath, branch)
		if err2 != nil {
			return "", "", fmt.Errorf("ensureTaskWorktree: git worktree add failed: %s / %s",
				strings.TrimSpace(out), strings.TrimSpace(out2))
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
	out, err := gitRunner.Combined(context.Background(), cwd, "worktree", "remove", "--force", worktreePath)
	if err != nil {
		return fmt.Errorf("removeTaskWorktree: %s: %w", strings.TrimSpace(out), err)
	}
	return nil
}
