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
