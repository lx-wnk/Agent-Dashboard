package db_test

import (
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
