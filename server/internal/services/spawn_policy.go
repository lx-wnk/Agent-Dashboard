package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// sensitiveHomeDirs are home-relative directory names that are unconditionally
// blacklisted as spawn working directories, regardless of any project roots.
// F-SEC-001 / CWE-269 / OWASP A04.
var sensitiveHomeDirs = []string{
	".ssh",
	".aws",
	".gnupg",
	".config",
	".claude",
}

// ErrCwdBlacklisted is returned when the requested cwd falls inside a
// sensitive home directory that is always blocked.
var ErrCwdBlacklisted = errors.New("cwd is inside a sensitive directory and cannot be used as a spawn working directory")

// ErrCwdNotAllowed is returned when the requested cwd does not fall under
// any registered project root.
var ErrCwdNotAllowed = errors.New("cwd outside allowed project roots")

// RootsProvider returns the set of allowed root paths. Each path should be an
// absolute, canonical path to a project folder. The SpawnPolicy canonicalises
// paths from the provider before comparison, so minor inconsistencies (trailing
// slashes, un-resolved symlinks) are tolerated.
type RootsProvider func(ctx context.Context) ([]string, error)

// SpawnPolicy decides whether a requested working directory is safe to use
// as the cwd of a spawned agent process.
type SpawnPolicy interface {
	// Allow returns nil when cwd is permitted, or a descriptive error otherwise.
	// The returned error is either ErrCwdBlacklisted or ErrCwdNotAllowed.
	Allow(ctx context.Context, cwd string) error
}

// spawnPolicy is the production implementation.
type spawnPolicy struct {
	roots RootsProvider // may be nil → no project-root restriction
}

// NewSpawnPolicy constructs a SpawnPolicy. roots provides the set of allowed
// project root paths; pass nil to enforce only the sensitive-dir blacklist (for
// use in dev/bypass-auth mode where no DB is available).
func NewSpawnPolicy(roots RootsProvider) SpawnPolicy {
	return &spawnPolicy{roots: roots}
}

// Allow implements SpawnPolicy.
func (p *spawnPolicy) Allow(ctx context.Context, cwd string) error {
	// Resolve to absolute, canonical path (follow symlinks).
	cwdAbs, err := filepath.Abs(cwd)
	if err != nil {
		return fmt.Errorf("cwd resolution failed: %w", err)
	}
	if real, err := filepath.EvalSymlinks(cwdAbs); err == nil {
		cwdAbs = real
	}

	// Unconditional blacklist — sensitive home directories always blocked.
	if err := checkBlacklist(cwdAbs); err != nil {
		return err
	}

	// When no roots provider is configured, skip the project-root check.
	if p.roots == nil {
		return nil
	}

	roots, err := p.roots(ctx)
	if err != nil {
		// Fail open on provider error so a DB hiccup doesn't block all spawns.
		// The blacklist above still applies.
		return nil
	}
	if len(roots) == 0 {
		// No project roots registered — only the blacklist applies.
		return nil
	}

	for _, root := range roots {
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		if real, err := filepath.EvalSymlinks(rootAbs); err == nil {
			rootAbs = real
		}
		if isUnder(cwdAbs, rootAbs) {
			return nil
		}
	}
	return ErrCwdNotAllowed
}

// checkBlacklist returns ErrCwdBlacklisted when cwdAbs falls inside one of the
// sensitive home directories. Fails open when the home directory cannot be
// determined.
func checkBlacklist(cwdAbs string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil // cannot determine home; skip blacklist
	}
	homeAbs, err := filepath.Abs(home)
	if err != nil {
		return nil
	}
	if real, err := filepath.EvalSymlinks(homeAbs); err == nil {
		homeAbs = real
	}

	for _, rel := range sensitiveHomeDirs {
		sensitive := filepath.Join(homeAbs, rel)
		if isUnder(cwdAbs, sensitive) {
			return ErrCwdBlacklisted
		}
	}
	return nil
}

// isUnder reports whether target is equal to root or is a descendant of root.
// Both paths must already be absolute; filepath.Clean is applied internally.
func isUnder(target, root string) bool {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	return target == root || strings.HasPrefix(target, root+string(filepath.Separator))
}

// ProjectFolderRootsProvider returns a RootsProvider that fetches all project
// folder paths from the database at call time. It lists all projects via
// projectRepo, then collects every folder path via folderRepo — suitable for
// small-to-medium deployments where the total number of registered folders is
// in the tens.
//
// When either repo is nil the provider fails open (returns nil, nil), leaving
// only the sensitive-dir blacklist active.
func ProjectFolderRootsProvider(projectRepo repo.ProjectRepo, folderRepo repo.ProjectFolderRepo) RootsProvider {
	return func(ctx context.Context) ([]string, error) {
		if projectRepo == nil || folderRepo == nil {
			return nil, nil
		}
		projects, err := projectRepo.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("spawn_policy: list projects: %w", err)
		}
		var paths []string
		for _, proj := range projects {
			folders, err := folderRepo.ListByProject(ctx, proj.ID)
			if err != nil {
				// Skip this project on error; don't block all spawns.
				continue
			}
			for _, f := range folders {
				if f.Path != "" {
					paths = append(paths, f.Path)
				}
			}
		}
		return paths, nil
	}
}
