package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// resolvedTmp returns the canonical (symlink-resolved) path to os.TempDir().
// On macOS os.TempDir() often returns /var/folders/... but the real path is
// under /private; tests that set HOME to a temp dir must use this value.
func resolvedTmp(t *testing.T) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks(TempDir): %v", err)
	}
	return p
}

func TestSpawnPolicy_BlacklistBlocksSSH(t *testing.T) {
	tmp := resolvedTmp(t)
	t.Setenv("HOME", tmp)

	policy := NewSpawnPolicy(nil)
	cwd := filepath.Join(tmp, ".ssh")
	_ = os.MkdirAll(cwd, 0o700)

	err := policy.Allow(context.Background(), cwd)
	if !errors.Is(err, ErrCwdBlacklisted) {
		t.Fatalf("expected ErrCwdBlacklisted for ~/.ssh, got %v", err)
	}
}

func TestSpawnPolicy_BlacklistBlocksAWS(t *testing.T) {
	tmp := resolvedTmp(t)
	t.Setenv("HOME", tmp)

	policy := NewSpawnPolicy(nil)
	cwd := filepath.Join(tmp, ".aws")
	_ = os.MkdirAll(cwd, 0o700)

	err := policy.Allow(context.Background(), cwd)
	if !errors.Is(err, ErrCwdBlacklisted) {
		t.Fatalf("expected ErrCwdBlacklisted for ~/.aws, got %v", err)
	}
}

func TestSpawnPolicy_BlacklistBlocksGnupg(t *testing.T) {
	tmp := resolvedTmp(t)
	t.Setenv("HOME", tmp)

	policy := NewSpawnPolicy(nil)
	cwd := filepath.Join(tmp, ".gnupg")
	_ = os.MkdirAll(cwd, 0o700)

	err := policy.Allow(context.Background(), cwd)
	if !errors.Is(err, ErrCwdBlacklisted) {
		t.Fatalf("expected ErrCwdBlacklisted for ~/.gnupg, got %v", err)
	}
}

func TestSpawnPolicy_BlacklistBlocksConfig(t *testing.T) {
	tmp := resolvedTmp(t)
	t.Setenv("HOME", tmp)

	policy := NewSpawnPolicy(nil)
	cwd := filepath.Join(tmp, ".config")
	_ = os.MkdirAll(cwd, 0o700)

	err := policy.Allow(context.Background(), cwd)
	if !errors.Is(err, ErrCwdBlacklisted) {
		t.Fatalf("expected ErrCwdBlacklisted for ~/.config, got %v", err)
	}
}

func TestSpawnPolicy_BlacklistBlocksClaude(t *testing.T) {
	tmp := resolvedTmp(t)
	t.Setenv("HOME", tmp)

	policy := NewSpawnPolicy(nil)
	cwd := filepath.Join(tmp, ".claude")
	_ = os.MkdirAll(cwd, 0o700)

	err := policy.Allow(context.Background(), cwd)
	if !errors.Is(err, ErrCwdBlacklisted) {
		t.Fatalf("expected ErrCwdBlacklisted for ~/.claude, got %v", err)
	}
}

func TestSpawnPolicy_BlacklistBlocksSubdirOfSensitive(t *testing.T) {
	tmp := resolvedTmp(t)
	t.Setenv("HOME", tmp)

	policy := NewSpawnPolicy(nil)
	// Subdirectory inside .ssh should also be blocked.
	cwd := filepath.Join(tmp, ".ssh", "keys")
	_ = os.MkdirAll(cwd, 0o700)

	err := policy.Allow(context.Background(), cwd)
	if !errors.Is(err, ErrCwdBlacklisted) {
		t.Fatalf("expected ErrCwdBlacklisted for ~/.ssh/keys, got %v", err)
	}
}

func TestSpawnPolicy_NilRoots_AllowsNonBlacklistedPath(t *testing.T) {
	tmp := resolvedTmp(t)
	t.Setenv("HOME", tmp)

	policy := NewSpawnPolicy(nil)
	cwd := filepath.Join(tmp, "myproject")
	_ = os.MkdirAll(cwd, 0o755)

	err := policy.Allow(context.Background(), cwd)
	if err != nil {
		t.Fatalf("expected nil error for non-blacklisted path with no roots provider, got %v", err)
	}
}

func TestSpawnPolicy_AllowsPathUnderRegisteredRoot(t *testing.T) {
	tmp := resolvedTmp(t)
	t.Setenv("HOME", tmp)

	projectRoot := filepath.Join(tmp, "myproject")
	_ = os.MkdirAll(projectRoot, 0o755)

	roots := func(_ context.Context) ([]string, error) {
		return []string{projectRoot}, nil
	}
	policy := NewSpawnPolicy(roots)

	// Exact match.
	if err := policy.Allow(context.Background(), projectRoot); err != nil {
		t.Fatalf("expected Allow for exact project root, got %v", err)
	}

	// Subdirectory.
	sub := filepath.Join(projectRoot, "src")
	_ = os.MkdirAll(sub, 0o755)
	if err := policy.Allow(context.Background(), sub); err != nil {
		t.Fatalf("expected Allow for subdirectory of project root, got %v", err)
	}
}

func TestSpawnPolicy_RejectsPathOutsideAllRoots(t *testing.T) {
	tmp := resolvedTmp(t)
	t.Setenv("HOME", tmp)

	projectRoot := filepath.Join(tmp, "myproject")
	_ = os.MkdirAll(projectRoot, 0o755)

	otherDir := filepath.Join(tmp, "otherdir")
	_ = os.MkdirAll(otherDir, 0o755)

	roots := func(_ context.Context) ([]string, error) {
		return []string{projectRoot}, nil
	}
	policy := NewSpawnPolicy(roots)

	err := policy.Allow(context.Background(), otherDir)
	if !errors.Is(err, ErrCwdNotAllowed) {
		t.Fatalf("expected ErrCwdNotAllowed for path outside registered roots, got %v", err)
	}
}

func TestSpawnPolicy_BlacklistTakesPrecedenceOverAllowedRoot(t *testing.T) {
	tmp := resolvedTmp(t)
	t.Setenv("HOME", tmp)

	// Even if someone registers ~/.ssh as a project root, it must still be blocked.
	sshDir := filepath.Join(tmp, ".ssh")
	_ = os.MkdirAll(sshDir, 0o700)

	roots := func(_ context.Context) ([]string, error) {
		return []string{sshDir}, nil
	}
	policy := NewSpawnPolicy(roots)

	err := policy.Allow(context.Background(), sshDir)
	if !errors.Is(err, ErrCwdBlacklisted) {
		t.Fatalf("expected ErrCwdBlacklisted even when ~/.ssh is registered as a root, got %v", err)
	}
}

func TestSpawnPolicy_EmptyRoots_AllowsNonBlacklistedPath(t *testing.T) {
	tmp := resolvedTmp(t)
	t.Setenv("HOME", tmp)

	roots := func(_ context.Context) ([]string, error) {
		return []string{}, nil
	}
	policy := NewSpawnPolicy(roots)

	cwd := filepath.Join(tmp, "myproject")
	_ = os.MkdirAll(cwd, 0o755)

	// Empty roots list means no project-root restriction.
	if err := policy.Allow(context.Background(), cwd); err != nil {
		t.Fatalf("expected Allow when roots list is empty, got %v", err)
	}
}

func TestSpawnPolicy_RootsProviderError_FailsOpen(t *testing.T) {
	tmp := resolvedTmp(t)
	t.Setenv("HOME", tmp)

	roots := func(_ context.Context) ([]string, error) {
		return nil, errors.New("db unavailable")
	}
	policy := NewSpawnPolicy(roots)

	// Non-blacklisted path — should be allowed when provider errors out.
	cwd := filepath.Join(tmp, "myproject")
	_ = os.MkdirAll(cwd, 0o755)

	if err := policy.Allow(context.Background(), cwd); err != nil {
		t.Fatalf("expected fail-open when roots provider errors, got %v", err)
	}
}

func TestIsUnder_ExactMatch(t *testing.T) {
	if !isUnder("/a/b", "/a/b") {
		t.Fatal("exact match must return true")
	}
}

func TestIsUnder_ChildDirectory(t *testing.T) {
	if !isUnder("/a/b/c", "/a/b") {
		t.Fatal("child directory must return true")
	}
}

func TestIsUnder_AdjacentPrefix(t *testing.T) {
	// /a/bc must NOT be considered under /a/b (path-separator boundary matters).
	if isUnder("/a/bc", "/a/b") {
		t.Fatal("adjacent prefix without separator must return false")
	}
}

func TestIsUnder_ParentDirectory(t *testing.T) {
	if isUnder("/a", "/a/b") {
		t.Fatal("parent must not be under child")
	}
}
