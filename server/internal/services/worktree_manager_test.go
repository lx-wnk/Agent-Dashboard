package services_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/services"
)

// initGitRepo creates a bare git repo with an initial commit so that worktree
// operations have something to branch from. Uses -c flags to avoid GPG signing.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	run("config", "commit.gpgsign", "false")
	// Create an initial commit so HEAD is valid for worktree add.
	f := filepath.Join(dir, "README")
	if err := os.WriteFile(f, []byte("init"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "README")
	run("commit", "-m", "init")
}

func newWTManager(t *testing.T) (*services.WorktreeManager, repo.TaskRepo) {
	t.Helper()
	// Redirect the worktree root (derived from $HOME) into a temp dir so created
	// worktrees auto-clean and never collide with a developer's real ~/dashboard-worktrees.
	t.Setenv("HOME", t.TempDir())
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = bundle.Client.Close() })
	taskRepo := repo.NewTaskRepo(bundle.Client)
	mgr := services.NewWorktreeManager(taskRepo)
	return mgr, taskRepo
}

func createTestTask(t *testing.T, taskRepo repo.TaskRepo, slug, cwd string) string {
	t.Helper()
	task, err := taskRepo.Create(t.Context(), repo.CreateTaskInput{
		Slug:                slug,
		Title:               "T-" + slug,
		Cwd:                 cwd,
		CurrentStage:        "concept",
		Priority:            "medium",
		MaxIterations:       5,
		StageTimeoutSeconds: 300,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	return task.ID
}

func TestCreateWorktree_Fresh(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found")
	}

	repoDir := t.TempDir()
	initGitRepo(t, repoDir)

	mgr, taskRepo := newWTManager(t)
	id := createTestTask(t, taskRepo, "fresh-wt", repoDir)

	path, err := mgr.CreateWorktree(t.Context(), id)
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	if path == "" {
		t.Fatal("expected non-empty path")
	}

	// Dir must exist.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("worktree dir not found: %v", err)
	}

	// Path must be persisted on the task.
	task, err := taskRepo.GetByID(t.Context(), id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if task.WorktreePath == nil || *task.WorktreePath != path {
		t.Fatalf("WorktreePath not persisted: got %v", task.WorktreePath)
	}
}

func TestCreateWorktree_Idempotent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found")
	}

	repoDir := t.TempDir()
	initGitRepo(t, repoDir)

	mgr, taskRepo := newWTManager(t)
	id := createTestTask(t, taskRepo, "idem-wt", repoDir)

	path1, err := mgr.CreateWorktree(t.Context(), id)
	if err != nil {
		t.Fatalf("first CreateWorktree: %v", err)
	}

	path2, err := mgr.CreateWorktree(t.Context(), id)
	if err != nil {
		t.Fatalf("second CreateWorktree: %v", err)
	}
	if path1 != path2 {
		t.Fatalf("expected same path, got %q vs %q", path1, path2)
	}
}

func TestRemoveWorktree_Clean(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found")
	}

	repoDir := t.TempDir()
	initGitRepo(t, repoDir)

	mgr, taskRepo := newWTManager(t)
	id := createTestTask(t, taskRepo, "rm-clean", repoDir)

	path, err := mgr.CreateWorktree(t.Context(), id)
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	if err := mgr.RemoveWorktree(t.Context(), id, false); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}

	// Dir must be gone.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected worktree dir to be removed, stat: %v", err)
	}

	// Path must be cleared on the task.
	task, err := taskRepo.GetByID(t.Context(), id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if task.WorktreePath != nil && *task.WorktreePath != "" {
		t.Fatalf("expected WorktreePath cleared, got %q", *task.WorktreePath)
	}
}

func TestRemoveWorktree_DirtyNoForce(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found")
	}

	repoDir := t.TempDir()
	initGitRepo(t, repoDir)

	mgr, taskRepo := newWTManager(t)
	id := createTestTask(t, taskRepo, "rm-dirty-noforce", repoDir)

	path, err := mgr.CreateWorktree(t.Context(), id)
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	// Write an untracked file to make the worktree dirty.
	if err := os.WriteFile(filepath.Join(path, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	err = mgr.RemoveWorktree(t.Context(), id, false)
	if !errors.Is(err, services.ErrWorktreeDirty) {
		t.Fatalf("expected ErrWorktreeDirty, got %v", err)
	}
}

func TestRemoveWorktree_DirtyForce(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found")
	}

	repoDir := t.TempDir()
	initGitRepo(t, repoDir)

	mgr, taskRepo := newWTManager(t)
	id := createTestTask(t, taskRepo, "rm-dirty-force", repoDir)

	path, err := mgr.CreateWorktree(t.Context(), id)
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	if err := os.WriteFile(filepath.Join(path, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := mgr.RemoveWorktree(t.Context(), id, true); err != nil {
		t.Fatalf("RemoveWorktree(force): %v", err)
	}

	task, err := taskRepo.GetByID(t.Context(), id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if task.WorktreePath != nil && *task.WorktreePath != "" {
		t.Fatalf("expected WorktreePath cleared, got %q", *task.WorktreePath)
	}
}

func TestRemoveWorktree_NoWorktree(t *testing.T) {
	mgr, taskRepo := newWTManager(t)
	id := createTestTask(t, taskRepo, "no-wt", "/tmp")

	err := mgr.RemoveWorktree(t.Context(), id, false)
	if !errors.Is(err, services.ErrNoWorktree) {
		t.Fatalf("expected ErrNoWorktree, got %v", err)
	}
}
