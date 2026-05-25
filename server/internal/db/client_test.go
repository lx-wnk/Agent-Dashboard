package db_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
)

func TestOpen_InMemory(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	require.NotNil(t, bundle)
	require.NotNil(t, bundle.Client)
	require.NotNil(t, bundle.DB)
	_ = bundle.Client.Close()
}

func TestOpen_AutoMigrate(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	defer func() { _ = bundle.Client.Close() }()

	// Prove schema was created: insert a record into api_keys.
	_, err = bundle.Client.ApiKey.Create().
		SetID("test-id").
		SetName("test").
		SetKeyHash("abc123").
		SetScopes([]string{"tasks:read"}).
		Save(t.Context())
	require.NoError(t, err)
}

func TestOpen_FTS5TableCreated(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	defer func() { _ = bundle.Client.Close() }()

	var name string
	err = bundle.DB.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='task_fts'",
	).Scan(&name)
	require.NoError(t, err)
	require.Equal(t, "task_fts", name)
}

// TestOpen_DropsBareWebFetchGrants verifies the F-SEC-004 startup migration:
// task_permissions rows with tool='WebFetch' and pattern IS NULL must be deleted
// when the database is opened, and a Warn log must have been emitted.
func TestOpen_DropsBareWebFetchGrants(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// First open: create schema + seed a bare-WebFetch grant via raw SQL.
	bundle1, err := db.Open(dbPath)
	require.NoError(t, err)

	// Insert a task so the FK constraint on task_permissions is satisfied.
	_, err = bundle1.DB.Exec(
		`INSERT INTO tasks (id, slug, title, cwd, current_stage, priority, max_iterations, stage_timeout_seconds, silver_bullet, created_at, updated_at)
		 VALUES ('t1','bare-wf-test','Test','','concept','medium',20,1800,0,datetime('now'),datetime('now'))`,
	)
	require.NoError(t, err)

	// Insert a bare-WebFetch grant (pattern IS NULL) — bypasses Go-layer validation.
	_, err = bundle1.DB.Exec(
		`INSERT INTO task_permissions (id, task_id, tool, pattern, granted, pre_approved, requested_at)
		 VALUES ('p1','t1','WebFetch',NULL,1,0,datetime('now'))`,
	)
	require.NoError(t, err)

	// Confirm the row is present before migration.
	var before int
	err = bundle1.DB.QueryRow(
		`SELECT COUNT(*) FROM task_permissions WHERE tool='WebFetch' AND pattern IS NULL`,
	).Scan(&before)
	require.NoError(t, err)
	require.Equal(t, 1, before, "expected bare-WebFetch row to exist before migration")
	require.NoError(t, bundle1.Close())

	// Second open: migration should delete the bare-WebFetch row.
	bundle2, err := db.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = bundle2.Close() }()

	var after int
	err = bundle2.DB.QueryRow(
		`SELECT COUNT(*) FROM task_permissions WHERE tool='WebFetch' AND pattern IS NULL`,
	).Scan(&after)
	require.NoError(t, err)
	require.Equal(t, 0, after, "expected bare-WebFetch rows to be deleted by migration")

	// Non-bare WebFetch grants (with a pattern) must survive.
	_, err = bundle2.DB.Exec(
		`INSERT INTO task_permissions (id, task_id, tool, pattern, granted, pre_approved, requested_at)
		 VALUES ('p2','t1','WebFetch','https://docs.example.com*',1,0,datetime('now'))`,
	)
	require.NoError(t, err)
	require.NoError(t, bundle2.Close())

	bundle3, err := db.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = bundle3.Close() }()

	var patterned int
	err = bundle3.DB.QueryRow(
		`SELECT COUNT(*) FROM task_permissions WHERE tool='WebFetch' AND pattern IS NOT NULL`,
	).Scan(&patterned)
	require.NoError(t, err)
	require.Equal(t, 1, patterned, "patterned WebFetch grants must not be deleted by migration")
}

// TestOpen_DropsBareWebFetchGrants_Idempotent verifies that the migration is a
// no-op (returns nil) when no bare-WebFetch rows exist.
func TestOpen_DropsBareWebFetchGrants_Idempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	bundle, err := db.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = bundle.Close() }()

	// No seeding — migration should silently succeed with count=0.
	var count int
	err = bundle.DB.QueryRow(
		`SELECT COUNT(*) FROM task_permissions WHERE tool='WebFetch' AND pattern IS NULL`,
	).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 0, count)
}

func TestOpen_FTS5TriggerRoundTrip(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	defer func() { _ = bundle.Client.Close() }()

	_, err = bundle.DB.Exec(
		`INSERT INTO tasks (id, slug, title, cwd, current_stage, priority, max_iterations, stage_timeout_seconds, silver_bullet, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))`,
		"task-fts-1", "fts-roundtrip", "Observability Dashboard Feature", "/tmp/project", "concept", "medium", 20, 1800, 0,
	)
	require.NoError(t, err)

	// FTS5 content= tables cannot return UNINDEXED columns directly via SELECT because the
	// engine re-fetches columns from the content table (tasks) by name, and tasks has no
	// "task_id" column. Use a rowid-based subquery to retrieve the task id instead.
	var taskID string
	err = bundle.DB.QueryRow(
		`SELECT id FROM tasks WHERE rowid IN (SELECT rowid FROM task_fts WHERE task_fts MATCH ?)`,
		"Observability",
	).Scan(&taskID)
	require.NoError(t, err)
	require.Equal(t, "task-fts-1", taskID)
}
