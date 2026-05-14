package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// overrideClaudeRoot temporarily replaces the claudeRoot function for the
// duration of the test and restores it via t.Cleanup.
func overrideClaudeRoot(t *testing.T, root string) {
	t.Helper()
	orig := claudeRoot
	claudeRoot = func() string { return root }
	t.Cleanup(func() { claudeRoot = orig })
}

// setupFakeClaudeRoot creates a temp directory that mirrors the ~/.claude
// structure expected by safePath, overrides claudeRoot(), and returns the
// real (symlink-resolved) fake ~/.claude path.
//
// On macOS t.TempDir() returns a path under /var/folders/... which is a
// symlink to /private/var/folders/.... filepath.EvalSymlinks inside safePath
// resolves the parent to the real path, so we must ensure claudeRoot() also
// returns the real path — otherwise the prefix checks inside safePath fail.
func setupFakeClaudeRoot(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	fakeClaudeDir := filepath.Join(tmp, ".claude")
	// Create project memory directories so EvalSymlinks succeeds on the parent.
	for _, proj := range []string{"myproject", "my-project.v2"} {
		memDir := filepath.Join(fakeClaudeDir, "projects", proj, "memory")
		if err := os.MkdirAll(memDir, 0o755); err != nil {
			t.Fatalf("setupFakeClaudeRoot: mkdir %s: %v", memDir, err)
		}
	}
	// Resolve any symlinks in the temp path (needed on macOS where /var → /private/var).
	realClaudeDir, err := filepath.EvalSymlinks(fakeClaudeDir)
	if err != nil {
		t.Fatalf("setupFakeClaudeRoot: EvalSymlinks: %v", err)
	}
	overrideClaudeRoot(t, realClaudeDir)
	return realClaudeDir
}

func TestSafePath(t *testing.T) {
	fakeRoot := setupFakeClaudeRoot(t)
	claudeBase := filepath.Join(fakeRoot, "projects")

	tests := []struct {
		name       string
		input      string
		wantEmpty  bool
		wantSuffix string // when non-empty: resolved path must end with this segment
	}{
		{
			name:       "valid path returns non-empty absolute path ending in notes.md",
			input:      "projects/myproject/memory/notes.md",
			wantEmpty:  false,
			wantSuffix: "notes.md",
		},
		{
			name:      "path traversal via dotdot is rejected",
			input:     "projects/../../../etc/passwd",
			wantEmpty: true,
		},
		{
			name:      "url-encoded traversal is rejected",
			input:     "projects/%2e%2e%2f%2e%2e%2fetc/passwd",
			wantEmpty: true,
		},
		{
			name:      "wrong segment count (3 segments) is rejected",
			input:     "projects/foo/memory",
			wantEmpty: true,
		},
		{
			name:      "wrong segment count (5 segments) is rejected",
			input:     "projects/foo/memory/file.md/extra",
			wantEmpty: true,
		},
		{
			name:      "wrong head segment is rejected",
			input:     "notprojects/foo/memory/file.md",
			wantEmpty: true,
		},
		{
			name:      "wrong dir segment is rejected",
			input:     "projects/foo/notmemory/file.md",
			wantEmpty: true,
		},
		{
			name:      "dotdot in project name is rejected because segment regex fails",
			input:     "projects/foo/../bar/memory/file.md",
			wantEmpty: true,
		},
		{
			name:      "file without .md extension is rejected",
			input:     "projects/myproject/memory/file.txt",
			wantEmpty: true,
		},
		{
			name:      "bare .md filename is rejected",
			input:     "projects/myproject/memory/.md",
			wantEmpty: true,
		},
		{
			name:       "valid project name with dots and hyphens is accepted",
			input:      "projects/my-project.v2/memory/notes.md",
			wantEmpty:  false,
			wantSuffix: "notes.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := safePath(tt.input)

			if tt.wantEmpty {
				if got != "" {
					t.Errorf("safePath(%q) = %q, want empty string", tt.input, got)
				}
				return
			}

			if got == "" {
				t.Fatalf("safePath(%q) = \"\", want non-empty path", tt.input)
			}
			if !filepath.IsAbs(got) {
				t.Errorf("safePath(%q) = %q, want absolute path", tt.input, got)
			}
			if tt.wantSuffix != "" && !strings.HasSuffix(got, tt.wantSuffix) {
				t.Errorf("safePath(%q) = %q, want path ending in %q", tt.input, got, tt.wantSuffix)
			}
			// Must be rooted under the fake ~/.claude/projects.
			if !strings.HasPrefix(got, claudeBase+string(filepath.Separator)) {
				t.Errorf("safePath(%q) = %q, want path under %q", tt.input, got, claudeBase)
			}
		})
	}
}
