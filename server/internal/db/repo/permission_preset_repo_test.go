package repo_test

import (
	"context"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/stretchr/testify/require"
)

// TestPermissionPresetRepo_UpsertNilPattern verifies that calling Upsert twice
// with an identical input that has a nil Pattern does NOT create duplicate rows.
//
// SQLite treats two NULL values as distinct in a UNIQUE index, so naive
// INSERT OR IGNORE semantics would silently create duplicates. The repo works
// around this with a manual existence check inside a transaction.
func TestPermissionPresetRepo_UpsertNilPattern(t *testing.T) {
	client := openTestDB(t)
	r := repo.NewPermissionPresetRepo(client)

	input := repo.UpsertPresetInput{
		UserID:     nil,
		ProjectCwd: "/projects/myapp",
		Tool:       "Read",
		Pattern:    nil,
	}

	// First upsert — should insert.
	err := r.Upsert(context.Background(), input)
	require.NoError(t, err)

	// Second upsert — identical input; should be a no-op, not insert a second row.
	err = r.Upsert(context.Background(), input)
	require.NoError(t, err)

	// Verify exactly one row exists.
	summaries, err := r.ListSummaries(context.Background(), nil)
	require.NoError(t, err)

	var found int
	for _, s := range summaries {
		if s.ProjectCwd == input.ProjectCwd {
			for _, e := range s.Entries {
				if e.Tool == input.Tool && e.Pattern == nil {
					found++
				}
			}
		}
	}
	require.Equal(t, 1, found,
		"Upsert with nil Pattern must deduplicate: expected 1 row, got %d", found)
}

// TestPermissionPresetRepo_UpsertWithPattern verifies normal deduplication
// for the common case where Pattern is non-nil.
func TestPermissionPresetRepo_UpsertWithPattern(t *testing.T) {
	client := openTestDB(t)
	r := repo.NewPermissionPresetRepo(client)
	userID := "user-123"

	input := repo.UpsertPresetInput{
		UserID:     &userID,
		ProjectCwd: "/projects/myapp",
		Tool:       "Bash",
		Pattern:    strPtr("git *"),
	}

	require.NoError(t, r.Upsert(context.Background(), input))
	require.NoError(t, r.Upsert(context.Background(), input))

	summaries, err := r.ListSummaries(context.Background(), &userID)
	require.NoError(t, err)

	var found int
	for _, s := range summaries {
		if s.ProjectCwd == input.ProjectCwd {
			for _, e := range s.Entries {
				if e.Tool == "Bash" {
					found++
				}
			}
		}
	}
	require.Equal(t, 1, found,
		"Upsert with non-nil Pattern must deduplicate: expected 1 row, got %d", found)
}

func strPtr(s string) *string { return &s }
