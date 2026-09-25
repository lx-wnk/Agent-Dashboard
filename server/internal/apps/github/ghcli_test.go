package github_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lx-wnk/kontor/server/internal/apps/github"
)

// An absent gh must be its own error, not a generic exec failure: the fix
// ("install the GitHub CLI") is different from the fix for a logged-out one.
func TestTokenFromGhCLIReportsAMissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := github.TokenFromGhCLI(t.Context())

	if !errors.Is(err, github.ErrGhNotFound) {
		t.Fatalf("err = %v, want ErrGhNotFound", err)
	}
}

// A gh that exits non-zero must surface gh's own stderr, which says what to
// run. Faked by putting a failing `gh` on PATH.
func TestTokenFromGhCLISurfacesWhatGhSaid(t *testing.T) {
	dir := t.TempDir()
	writeFakeGh(t, dir, "#!/bin/sh\necho 'please run: gh auth login' >&2\nexit 1\n")
	t.Setenv("PATH", dir)

	_, err := github.TokenFromGhCLI(t.Context())

	if err == nil || !contains(err.Error(), "gh auth login") {
		t.Fatalf("err = %v, want gh's own advice", err)
	}
}

// An authenticated gh prints the token and nothing else; trailing newline must
// not travel into an Authorization header.
func TestTokenFromGhCLITrimsTheToken(t *testing.T) {
	dir := t.TempDir()
	writeFakeGh(t, dir, "#!/bin/sh\necho 'gho_abc123'\n")
	t.Setenv("PATH", dir)

	token, err := github.TokenFromGhCLI(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "gho_abc123" {
		t.Fatalf("token = %q, want %q", token, "gho_abc123")
	}
}

// gh exiting 0 with nothing is not a token, and must not be handed to GitHub
// as an empty Authorization header.
func TestTokenFromGhCLIRejectsAnEmptyAnswer(t *testing.T) {
	dir := t.TempDir()
	writeFakeGh(t, dir, "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", dir)

	if _, err := github.TokenFromGhCLI(t.Context()); err == nil {
		t.Fatal("empty output must be an error")
	}
}

// First call times out (slow keychain unlock); second succeeds. The retry
// must surface the token, not the timeout error.
func TestTokenFromGhCLIRetriesOnceOnTimeout(t *testing.T) {
	dir := t.TempDir()
	flag := filepath.Join(dir, "called")
	// First invocation creates the flag file then sleeps forever (killed by
	// the 1s timeout). Second invocation sees the flag and prints the token.
	// touch/sleep are external — keep /usr/bin:/bin on PATH.
	script := fmt.Sprintf(`#!/bin/sh
if [ -f %q ]; then
  echo "gho_retry_ok"
  exit 0
fi
touch %q
exec sleep 10
`, flag, flag)
	writeFakeGh(t, dir, script)
	t.Cleanup(github.SetGhTokenTimeout(1 * time.Second))
	t.Setenv("PATH", dir+":/usr/bin:/bin")

	start := time.Now()
	token, err := github.TokenFromGhCLI(t.Context())
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected token, got error: %v", err)
	}
	if token != "gho_retry_ok" {
		t.Fatalf("token = %q, want %q", token, "gho_retry_ok")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("took %v, want < 2s (timeout should be ~200ms per attempt)", elapsed)
	}
}

func writeFakeGh(t *testing.T, dir, script string) {
	t.Helper()
	t.Cleanup(github.SetGhTokenTimeout(time.Minute))
	path := dir + "/gh"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatal(err)
	}
}

func contains(haystack, needle string) bool { return strings.Contains(haystack, needle) }
