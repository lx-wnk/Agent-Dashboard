// Package worktree holds stdlib-only primitives for deriving git-worktree
// locations/branches and running git commands with a bounded timeout. It is a
// leaf: both the lifecycle layer (pipeline) and the inspection layer (services)
// consume it, but it imports nothing from the project itself.
package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const (
	// DefaultRootDirName is the directory under $HOME that holds auto-created
	// worktrees when no explicit root is configured.
	DefaultRootDirName = "dashboard-worktrees"
	// BranchPrefix is prepended to a task slug when no source branch is set.
	BranchPrefix = "feat/"
)

const defaultTimeout = 15 * time.Second

// Runner executes git commands with a bounded per-call timeout.
type Runner struct {
	bin     string
	timeout time.Duration
}

// NewRunner resolves the git binary via PATH (falling back to "git") and
// returns a Runner with the default 15s timeout.
func NewRunner() *Runner {
	bin, err := exec.LookPath("git")
	if err != nil || bin == "" {
		bin = "git"
	}
	return &Runner{bin: bin, timeout: defaultTimeout}
}

// Output runs git in cwd and returns stdout only — for read-only inspection
// where stderr noise would corrupt the parsed value.
func (r *Runner) Output(ctx context.Context, cwd string, args ...string) (string, error) {
	return r.run(ctx, cwd, false, args...)
}

// Combined runs git in cwd and returns stdout+stderr merged — for mutations
// where the error message must surface git's diagnostics.
func (r *Runner) Combined(ctx context.Context, cwd string, args ...string) (string, error) {
	return r.run(ctx, cwd, true, args...)
}

func (r *Runner) run(ctx context.Context, cwd string, combined bool, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.bin, args...)
	cmd.Dir = cwd
	if combined {
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	out, err := cmd.Output()
	return string(out), err
}

// DefaultRoot returns root unchanged when set, else $HOME/dashboard-worktrees.
func DefaultRoot(root string) string {
	if root != "" {
		return root
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, DefaultRootDirName)
}

// PathFor returns the worktree directory for slug under the resolved root.
func PathFor(root, slug string) string {
	return filepath.Join(DefaultRoot(root), slug)
}

// CreateBranch returns *sourceBranch when set and non-empty, else BranchPrefix+slug.
func CreateBranch(sourceBranch *string, slug string) string {
	if sourceBranch != nil && *sourceBranch != "" {
		return *sourceBranch
	}
	return BranchPrefix + slug
}
