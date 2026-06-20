package worktree

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultRoot(t *testing.T) {
	if got := DefaultRoot("/explicit/root"); got != "/explicit/root" {
		t.Fatalf("explicit root: got %q", got)
	}
	got := DefaultRoot("")
	if !strings.HasSuffix(got, "/"+DefaultRootDirName) {
		t.Fatalf("empty root: expected suffix /%s, got %q", DefaultRootDirName, got)
	}
}

func TestPathFor(t *testing.T) {
	if got := PathFor("/root", "my-task"); got != "/root/my-task" {
		t.Fatalf("got %q", got)
	}
	if got := PathFor("", "my-task"); !strings.HasSuffix(got, "/"+DefaultRootDirName+"/my-task") {
		t.Fatalf("empty root: got %q", got)
	}
}

func TestCreateBranch(t *testing.T) {
	set := "feat/explicit"
	empty := ""
	cases := []struct {
		name   string
		source *string
		slug   string
		want   string
	}{
		{"nil source derives slug", nil, "my-task", "feat/my-task"},
		{"empty source derives slug", &empty, "my-task", "feat/my-task"},
		{"set source used verbatim", &set, "my-task", "feat/explicit"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CreateBranch(tc.source, tc.slug); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRunnerOutput(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available:", err)
	}
	repoDir := t.TempDir()
	if err := exec.Command("git", "-C", repoDir, "init").Run(); err != nil {
		t.Skip("git init failed:", err)
	}

	out, err := NewRunner().Output(context.Background(), repoDir, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		t.Fatalf("Output: %v", err)
	}
	if strings.TrimSpace(out) != "true" {
		t.Fatalf("expected true, got %q", out)
	}
}

func TestParseWorktreeBranches(t *testing.T) {
	porcelain := "worktree /repo/main\nHEAD aaaa\nbranch refs/heads/main\n\n" +
		"worktree /repo/wt-feat\nHEAD bbbb\nbranch refs/heads/feat/x\n\n" +
		"worktree /repo/detached\nHEAD cccc\ndetached\n\n"
	got := parseWorktreeBranches(porcelain)
	if got["main"] != "/repo/main" {
		t.Fatalf("main: got %q", got["main"])
	}
	if got["feat/x"] != "/repo/wt-feat" {
		t.Fatalf("feat/x: got %q", got["feat/x"])
	}
	if _, ok := got["detached"]; ok {
		t.Fatal("detached worktree must not produce a branch entry")
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 branches, got %d: %v", len(got), got)
	}
}

func TestBranchCheckedOutAt(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available:", err)
	}
	repoDir := t.TempDir()
	run := func(args ...string) {
		full := append([]string{"-C", repoDir, "-c", "commit.gpgsign=false"}, args...)
		cmd := exec.Command("git", full...)
		cmd.Env = append(cmd.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v failed: %v (%s)", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("commit", "--allow-empty", "-m", "init")
	wtPath := filepath.Join(t.TempDir(), "wt")
	run("worktree", "add", "-b", "feat/held", wtPath)

	ctx := context.Background()
	path, err := BranchCheckedOutAt(ctx, repoDir, "feat/held")
	if err != nil {
		t.Fatalf("BranchCheckedOutAt: %v", err)
	}
	// git may report the symlink-resolved path (e.g. /private/var on macOS), so
	// match by basename rather than the exact temp path.
	if filepath.Base(path) != filepath.Base(wtPath) || path == "" {
		t.Fatalf("held branch: got %q want suffix %q", path, wtPath)
	}
	if free, _ := BranchCheckedOutAt(ctx, repoDir, "feat/unused"); free != "" {
		t.Fatalf("unused branch must be free, got %q", free)
	}
}

func TestRunnerCombinedSurfacesError(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available:", err)
	}
	notRepo := filepath.Join(t.TempDir(), "empty")
	out, err := NewRunner().Combined(context.Background(), t.TempDir(), "worktree", "add", notRepo, "nope")
	if err == nil {
		t.Fatal("expected error running git outside a repo")
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("expected git diagnostics in combined output")
	}
}
