package pipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
)

func TestEnsureTaskWorktree_NoSourceBranch(t *testing.T) {
	task := &ent.Task{Slug: "test-task", Cwd: t.TempDir()}
	_, err := ensureTaskWorktree(task, t.TempDir())
	if err == nil {
		t.Fatal("expected error when SourceBranch is nil")
	}
}

func TestEnsureTaskWorktree_CreatesAndIdempotent(t *testing.T) {
	repoDir := t.TempDir()
	if err := exec.Command("git", "-C", repoDir, "init").Run(); err != nil {
		t.Skip("git not available:", err)
	}
	// Need at least one commit for worktree add to work.
	exec.Command("git", "-C", repoDir, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", repoDir, "config", "user.name", "Test").Run()
	exec.Command("git", "-C", repoDir, "commit", "--allow-empty", "-m", "init").Run()

	branch := "feat/auto-worktree-test"
	task := &ent.Task{Slug: "wt-test", Cwd: repoDir, SourceBranch: &branch}
	root := filepath.Join(t.TempDir(), "worktrees")

	// First call: creates worktree.
	path1, err := ensureTaskWorktree(task, root)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if _, err := os.Stat(path1); err != nil {
		t.Fatalf("worktree directory does not exist: %v", err)
	}

	// Second call: idempotent (directory exists → returns immediately).
	path2, err := ensureTaskWorktree(task, root)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if path1 != path2 {
		t.Fatalf("idempotent: expected same path %s, got %s", path1, path2)
	}
}

func TestRemoveTaskWorktree_EmptyPath(t *testing.T) {
	// removeTaskWorktree with empty path is a no-op.
	if err := removeTaskWorktree("/some/cwd", ""); err != nil {
		t.Fatal("expected nil for empty path, got:", err)
	}
}
