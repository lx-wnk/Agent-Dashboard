package pipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
)

func initTestRepo(t *testing.T, repoDir string) {
	t.Helper()
	if err := exec.Command("git", "-C", repoDir, "init").Run(); err != nil {
		t.Skip("git not available:", err)
	}
	for _, args := range [][]string{
		{"config", "user.email", "test@test.com"},
		{"config", "user.name", "Test"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
		cmd := append([]string{"-C", repoDir}, args...)
		if err := exec.Command("git", cmd...).Run(); err != nil {
			t.Fatalf("git %v failed: %v", args, err)
		}
	}
}

func TestEnsureTaskWorktree_NoSourceBranch_DerivesSlug(t *testing.T) {
	repoDir := t.TempDir()
	initTestRepo(t, repoDir)

	task := &ent.Task{Slug: "my-task", Cwd: repoDir}
	root := filepath.Join(t.TempDir(), "worktrees")

	path, branch, err := ensureTaskWorktree(task, root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if branch != "feat/my-task" {
		t.Fatalf("expected derived branch feat/my-task, got %s", branch)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("worktree directory does not exist: %v", err)
	}
}

func TestEnsureTaskWorktree_CreatesAndIdempotent(t *testing.T) {
	repoDir := t.TempDir()
	initTestRepo(t, repoDir)

	branch := "feat/auto-worktree-test"
	task := &ent.Task{Slug: "wt-test", Cwd: repoDir, SourceBranch: &branch}
	root := filepath.Join(t.TempDir(), "worktrees")

	// First call: creates worktree.
	path1, _, err := ensureTaskWorktree(task, root)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if _, err := os.Stat(path1); err != nil {
		t.Fatalf("worktree directory does not exist: %v", err)
	}

	// Second call: idempotent (directory exists → returns immediately).
	path2, _, err := ensureTaskWorktree(task, root)
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
