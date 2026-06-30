// Package checkpoint captures per-turn worktree snapshots into hidden git refs
// (refs/checkpoints/<taskID>/<seq>) and restores a worktree to any snapshot.
// All snapshots use a temporary git index so the agent's real index and HEAD are
// never touched.
package checkpoint

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const snapshotTimeout = 60 * time.Second

// hardSkipPathspecs are git pathspecs that exclude heavy/irrelevant directories
// from a snapshot regardless of the worktree's .gitignore. The glob excludes match
// node_modules/dist at any depth (nested package dirs too), mirroring the watcher's
// substring shouldIgnore.
var hardSkipPathspecs = []string{".", ":(exclude,glob)**/node_modules/**", ":(exclude,glob)**/dist/**", ":!.git"}

// SnapshotResult carries the output of a Snapshot call.
type SnapshotResult struct {
	TreeSHA      string
	CommitSHA    string
	FilesChanged int
	// Skipped is true when the tree is identical to prevTreeSHA — no new
	// checkpoint ref or commit was created.
	Skipped bool
}

// Snapshot captures the full worktree state (tracked + non-ignored untracked)
// into refs/checkpoints/<taskID>/<seq>. Identical trees (prevTreeSHA match) are
// skipped. See SnapshotWithParent for chaining checkpoint commits.
func Snapshot(ctx context.Context, worktreePath, taskID string, seq int, prevTreeSHA string) (SnapshotResult, error) {
	return SnapshotWithParent(ctx, worktreePath, taskID, seq, prevTreeSHA, "")
}

// SnapshotWithParent is Snapshot with an explicit parent commit for commit-tree,
// so checkpoint commits form a chain. prevTreeSHA drives the identical-tree skip;
// prevCommitSHA (when non-empty) becomes the new commit's parent.
func SnapshotWithParent(ctx context.Context, worktreePath, taskID string, seq int, prevTreeSHA, prevCommitSHA string) (SnapshotResult, error) {
	ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	defer cancel()

	// Temp index — never touch the real index.
	tmpPath, err := tempIndexFile()
	if err != nil {
		return SnapshotResult{}, fmt.Errorf("snapshot: create temp index: %w", err)
	}
	defer os.Remove(tmpPath)

	env := append(os.Environ(), "GIT_INDEX_FILE="+tmpPath)

	// Stage all files (tracked + non-ignored untracked) into the temp index.
	// hardSkipPathspecs hard-excludes .git/node_modules/dist even when the worktree
	// has no .gitignore, bounding tree size (spec D3).
	addArgs := append([]string{"-c", "core.hooksPath=/dev/null", "add", "-A", "--"}, hardSkipPathspecs...)
	if out, err := runGit(ctx, worktreePath, env, addArgs...); err != nil {
		return SnapshotResult{}, fmt.Errorf("snapshot: git add -A: %s: %w", out, err)
	}

	wtOut, err := runGit(ctx, worktreePath, env, "write-tree")
	if err != nil {
		return SnapshotResult{}, fmt.Errorf("snapshot: git write-tree: %s: %w", wtOut, err)
	}
	treeSHA := strings.TrimSpace(wtOut)

	if prevTreeSHA != "" && treeSHA == prevTreeSHA {
		return SnapshotResult{Skipped: true, TreeSHA: treeSHA}, nil
	}

	lsOut, _ := runGit(ctx, worktreePath, os.Environ(), "ls-tree", "-r", "--name-only", treeSHA)
	filesChanged := len(splitLines(lsOut))

	ctArgs := []string{"commit-tree", treeSHA, "-m", fmt.Sprintf("checkpoint: %s seq %d", taskID, seq)}
	if prevCommitSHA != "" {
		ctArgs = append(ctArgs, "-p", prevCommitSHA)
	}
	commitOut, err := runGit(ctx, worktreePath, os.Environ(), ctArgs...)
	if err != nil {
		return SnapshotResult{}, fmt.Errorf("snapshot: git commit-tree: %s: %w", commitOut, err)
	}
	commitSHA := strings.TrimSpace(commitOut)

	refName := fmt.Sprintf("refs/checkpoints/%s/%d", taskID, seq)
	if out, err := runGit(ctx, worktreePath, os.Environ(), "update-ref", refName, commitSHA); err != nil {
		return SnapshotResult{}, fmt.Errorf("snapshot: update-ref %s: %s: %w", refName, out, err)
	}

	return SnapshotResult{TreeSHA: treeSHA, CommitSHA: commitSHA, FilesChanged: filesChanged}, nil
}

// Restore makes the working tree at worktreePath exactly match treeSHA: files in
// the tree are written, files absent from the tree (tracked or untracked-non-ignored)
// are removed. HEAD and the branch ref are never touched. repoDir is the git repo
// root (may equal worktreePath).
func Restore(ctx context.Context, repoDir, worktreePath, treeSHA string) error {
	ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	defer cancel()

	lsOut, err := runGit(ctx, repoDir, os.Environ(), "ls-tree", "-r", "--name-only", treeSHA)
	if err != nil {
		return fmt.Errorf("restore: ls-tree: %s: %w", lsOut, err)
	}
	treeFiles := make(map[string]bool)
	for _, f := range splitLines(lsOut) {
		treeFiles[f] = true
	}

	trackedOut, _ := runGit(ctx, worktreePath, os.Environ(), "ls-files")
	untrackedOut, _ := runGit(ctx, worktreePath, os.Environ(), "ls-files", "--others", "--exclude-standard")
	for _, f := range append(splitLines(trackedOut), splitLines(untrackedOut)...) {
		if !treeFiles[f] {
			_ = os.Remove(filepath.Join(worktreePath, f))
		}
	}

	tmpPath, err := tempIndexFile()
	if err != nil {
		return fmt.Errorf("restore: create temp index: %w", err)
	}
	defer os.Remove(tmpPath)

	env := append(os.Environ(), "GIT_INDEX_FILE="+tmpPath)
	if out, err := runGit(ctx, worktreePath, env, "read-tree", treeSHA); err != nil {
		return fmt.Errorf("restore: read-tree: %s: %w", out, err)
	}

	prefix := worktreePath
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	if out, err := runGit(ctx, repoDir, env, "checkout-index", "-a", "-f", "--prefix="+prefix); err != nil {
		return fmt.Errorf("restore: checkout-index: %s: %w", out, err)
	}
	return nil
}

// DeleteCheckpointRefs removes all refs/checkpoints/<taskID>/* refs from the repo.
func DeleteCheckpointRefs(ctx context.Context, repoDir, taskID string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	out, err := runGit(ctx, repoDir, os.Environ(),
		"for-each-ref", "--format=%(refname)", "refs/checkpoints/"+taskID+"/")
	if err != nil || strings.TrimSpace(out) == "" {
		return nil
	}
	for _, ref := range splitLines(out) {
		if delOut, derr := runGit(ctx, repoDir, os.Environ(), "update-ref", "-d", ref); derr != nil {
			return fmt.Errorf("deleteCheckpointRefs: delete %s: %s: %w", ref, delOut, derr)
		}
	}
	return nil
}

// DeleteCheckpointRefSeqs deletes refs/checkpoints/<taskID>/<seq> for each seq.
// Used when pruning old checkpoint rows so the matching git refs (and their objects)
// don't accumulate until worktree teardown.
func DeleteCheckpointRefSeqs(ctx context.Context, repoDir, taskID string, seqs []int) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var errs []error
	for _, seq := range seqs {
		ref := fmt.Sprintf("refs/checkpoints/%s/%d", taskID, seq)
		if out, err := runGit(ctx, repoDir, os.Environ(), "update-ref", "-d", ref); err != nil {
			errs = append(errs, fmt.Errorf("delete %s: %s: %w", ref, out, err))
		}
	}
	return errors.Join(errs...)
}

// tempIndexFile returns a unique, non-existent path for a throwaway GIT_INDEX_FILE.
// git refuses to operate on a zero-byte index, so the reserved file is removed and
// git is left to create a fresh index at that path.
func tempIndexFile() (string, error) {
	tmp, err := os.CreateTemp("", "cp-idx-*")
	if err != nil {
		return "", err
	}
	path := tmp.Name()
	_ = tmp.Close()
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return path, nil
}

func runGit(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func splitLines(s string) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(s), "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}
