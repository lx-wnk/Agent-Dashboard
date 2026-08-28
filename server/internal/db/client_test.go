package db_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
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

func TestOpen_MigratesLegacyPipelineConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.db")

	// Seed the legacy pipeline_configs shape: (key, value) with key as PK, no id column.
	raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	require.NoError(t, err)
	_, err = raw.Exec("CREATE TABLE `pipeline_configs` (`key` text NOT NULL, `value` text NOT NULL, PRIMARY KEY (`key`))")
	require.NoError(t, err)
	_, err = raw.Exec("INSERT INTO pipeline_configs (key, value) VALUES ('maxAutoRetries','3'),('stageModel.implementation','claude-opus-4-8')")
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	// Open via the production path — must migrate without the NOT NULL id failure.
	bundle, err := db.Open(path)
	require.NoError(t, err)
	defer func() { _ = bundle.Client.Close() }()

	all, err := bundle.Client.PipelineConfig.Query().All(t.Context())
	require.NoError(t, err)
	require.Len(t, all, 2)
	byKey := map[string]string{}
	for _, c := range all {
		byKey[c.Key] = c.Value
		require.Equal(t, c.Key, c.ID)     // id backfilled from key
		require.Equal(t, "", c.ProjectID) // global scope sentinel
	}
	require.Equal(t, "3", byKey["maxAutoRetries"])
	require.Equal(t, "claude-opus-4-8", byKey["stageModel.implementation"])
}

func TestOpen_StageRunSessionIDIndex_FreshDB(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	defer func() { _ = bundle.Client.Close() }()

	require.True(t, hasIndex(t, bundle.DB, "stage_runs", "stagerun_session_id"))
}

// TestOpen_StageRunSessionIDIndex_ExistingDB seeds a pre-index stage_runs
// table (the shape before this migration) and confirms db.Open pre-seeds the
// index via CREATE INDEX IF NOT EXISTS so ent's auto-migrate sees it already
// present, rather than driving a table rebuild — the PR #207 rebuild-crash
// class of bug. Also confirms a second Open is a clean idempotent no-op.
func TestOpen_StageRunSessionIDIndex_ExistingDB(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "preindex.db")

	raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	require.NoError(t, err)
	_, err = raw.Exec(`CREATE TABLE stage_runs (
		id text NOT NULL,
		task_id text NOT NULL,
		stage text NOT NULL,
		session_id text NULL,
		session_name text NULL,
		pid integer NULL,
		status text NOT NULL DEFAULT 'pending',
		iteration integer NOT NULL DEFAULT 0,
		output json NULL,
		tokens_used integer NOT NULL DEFAULT 0,
		cost_cents integer NOT NULL DEFAULT 0,
		started_at datetime NULL,
		ended_at datetime NULL,
		last_grant_at datetime NULL,
		retry_count integer NOT NULL DEFAULT 0,
		next_retry_at datetime NULL,
		pending_user_prompt text NULL,
		created_at datetime NOT NULL DEFAULT (datetime('now')),
		PRIMARY KEY (id)
	)`)
	require.NoError(t, err)
	_, err = raw.Exec(`CREATE INDEX stagerun_status ON stage_runs (status)`)
	require.NoError(t, err)
	_, err = raw.Exec(`CREATE INDEX stagerun_task_id_stage_iteration ON stage_runs (task_id, stage, iteration)`)
	require.NoError(t, err)
	_, err = raw.Exec(`CREATE INDEX stagerun_task_id_created_at ON stage_runs (task_id, created_at)`)
	require.NoError(t, err)
	_, err = raw.Exec(`CREATE UNIQUE INDEX stagerun_task_id ON stage_runs (task_id) WHERE status = 'running'`)
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	require.False(t, hasIndexAtPath(t, path, "stage_runs", "stagerun_session_id"),
		"seeded DB must not already have the session_id index")

	bundle1, err := db.Open(path)
	require.NoError(t, err, "Open must not crash migrating a pre-index stage_runs table")
	require.True(t, hasIndex(t, bundle1.DB, "stage_runs", "stagerun_session_id"))
	require.NoError(t, bundle1.Close())

	// Idempotency: a second Open on the already-migrated file is a clean no-op.
	bundle2, err := db.Open(path)
	require.NoError(t, err)
	defer func() { _ = bundle2.Close() }()
	require.True(t, hasIndex(t, bundle2.DB, "stage_runs", "stagerun_session_id"))
}

func TestOpen_StageRunSessionIDIndex_QueryPlanUsesIndex(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	defer func() { _ = bundle.Client.Close() }()

	rows, err := bundle.DB.Query(`EXPLAIN QUERY PLAN SELECT * FROM stage_runs WHERE session_id = 'x'`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	var plan string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		require.NoError(t, rows.Scan(&id, &parent, &notUsed, &detail))
		plan += detail + "\n"
	}
	require.NoError(t, rows.Err())
	require.Contains(t, plan, "stagerun_session_id")
	require.NotContains(t, plan, "SCAN stage_runs")
}

func hasIndex(t *testing.T, sqlDB *sql.DB, table, indexName string) bool {
	t.Helper()
	rows, err := sqlDB.Query(fmt.Sprintf("PRAGMA index_list(%s)", table))
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var seq int
		var name string
		var unique, partial int
		var origin string
		require.NoError(t, rows.Scan(&seq, &name, &unique, &origin, &partial))
		if name == indexName {
			return true
		}
	}
	require.NoError(t, rows.Err())
	return false
}

func hasIndexAtPath(t *testing.T, path, table, indexName string) bool {
	t.Helper()
	raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	require.NoError(t, err)
	defer func() { _ = raw.Close() }()
	return hasIndex(t, raw, table, indexName)
}

// TestOpenTwiceWithResourceTable proves the resource table and its unique index
// survive a second Open on an existing database. An ent index change triggers
// SQLite's 12-step table rebuild, which fails on populated tables with
// "NOT NULL constraint failed" — the pre-migration exists to make ent's diff
// find the index already present so it never rebuilds.
func TestOpenTwiceWithResourceTable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "twice.db")

	first, err := db.Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	ctx := context.Background()
	if _, err := first.Client.Resource.Create().
		SetID("res-1").
		SetKind("application").
		SetSlug("example").
		Save(ctx); err != nil {
		t.Fatalf("insert resource: %v", err)
	}
	if err := first.Client.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}

	second, err := db.Open(path)
	if err != nil {
		t.Fatalf("second Open on a populated database: %v", err)
	}
	t.Cleanup(func() { _ = second.Client.Close() })

	n, err := second.Client.Resource.Query().Count(ctx)
	if err != nil {
		t.Fatalf("count after reopen: %v", err)
	}
	if n != 1 {
		t.Errorf("resource count after reopen = %d, want 1", n)
	}
}

// insertBackfillTask inserts the single task row task_permissions' FK
// requires, mirroring the fixture TestOpen_DropsBareWebFetchGrants already
// uses for the same purpose.
func insertBackfillTask(t *testing.T, sqlDB *sql.DB, id string) {
	t.Helper()
	_, err := sqlDB.Exec(
		`INSERT INTO tasks (id, slug, title, cwd, current_stage, priority, max_iterations, stage_timeout_seconds, silver_bullet, created_at, updated_at)
		 VALUES (?, ?, 'Test', '', 'concept', 'medium', 20, 1800, 0, datetime('now'), datetime('now'))`,
		id, id,
	)
	require.NoError(t, err)
}

// TestBackfillGrantsIsIdempotent seeds one task_permission and one
// permission_preset, reopens the database to trigger the backfill migration,
// and asserts the grant count is 2 — then reopens once more and asserts the
// count is still 2. A migration that runs on every boot must be a no-op once
// settled, and the second-run assertion is the only thing that proves it.
func TestBackfillGrantsIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	ctx := context.Background()

	bundle1, err := db.Open(dbPath)
	require.NoError(t, err)
	insertBackfillTask(t, bundle1.DB, "t1")

	pattern := "pnpm test"
	_, err = repo.NewPermissionRepo(bundle1.Client).CreateTaskPermission(ctx, repo.CreateTaskPermissionInput{
		TaskID: "t1", Tool: "Bash", Pattern: &pattern, Granted: true,
	})
	require.NoError(t, err)

	err = repo.NewPermissionPresetRepo(bundle1.Client).Upsert(ctx, repo.UpsertPresetInput{
		ProjectCwd: "/repo", Tool: "Read",
	})
	require.NoError(t, err)
	require.NoError(t, bundle1.Close())

	// Second open triggers the backfill migration over the seeded rows.
	bundle2, err := db.Open(dbPath)
	require.NoError(t, err)
	var count int
	require.NoError(t, bundle2.DB.QueryRow(`SELECT COUNT(*) FROM grants`).Scan(&count))
	require.Equal(t, 2, count, "expected one grant per backfilled row")
	require.NoError(t, bundle2.Close())

	// Third open must not duplicate the already-backfilled grants.
	bundle3, err := db.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = bundle3.Close() }()
	require.NoError(t, bundle3.DB.QueryRow(`SELECT COUNT(*) FROM grants`).Scan(&count))
	require.Equal(t, 2, count, "second migration run must not duplicate grants")
}

// TestBackfillGrantsMarksLegacyIdentity asserts every backfilled grant has
// granted_by == "migration:legacy". granted_by is required and the legacy
// rows carry no identity; an empty string would be indistinguishable from a
// bug, so the marker says "unknown because it predates identity" out loud.
func TestBackfillGrantsMarksLegacyIdentity(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	ctx := context.Background()

	bundle1, err := db.Open(dbPath)
	require.NoError(t, err)
	insertBackfillTask(t, bundle1.DB, "t1")

	pattern := "pnpm test"
	_, err = repo.NewPermissionRepo(bundle1.Client).CreateTaskPermission(ctx, repo.CreateTaskPermissionInput{
		TaskID: "t1", Tool: "Bash", Pattern: &pattern, Granted: true,
	})
	require.NoError(t, err)
	require.NoError(t, bundle1.Close())

	bundle2, err := db.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = bundle2.Close() }()

	rows, err := bundle2.DB.Query(`SELECT granted_by FROM grants`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	seen := 0
	for rows.Next() {
		var grantedBy string
		require.NoError(t, rows.Scan(&grantedBy))
		require.Equal(t, "migration:legacy", grantedBy)
		seen++
	}
	require.NoError(t, rows.Err())
	require.Greater(t, seen, 0, "expected at least one backfilled grant")
}

// TestBackfillGrantsSkipsUngrantedTaskPermissions proves an ungranted
// task_permission row (still pending, or denied) does not become an allow
// grant. Unlike permission_presets — which carry no such flag and are
// backfilled unconditionally — a task_permission row only reflects an actual
// decision when granted = 1; ListEffectiveTaskPermissions already treats
// granted = 0 the same way, and backfilling it as an allow grant would turn a
// denied or still-pending request into a retroactive allow.
func TestBackfillGrantsSkipsUngrantedTaskPermissions(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	ctx := context.Background()

	bundle1, err := db.Open(dbPath)
	require.NoError(t, err)
	insertBackfillTask(t, bundle1.DB, "t1")

	_, err = repo.NewPermissionRepo(bundle1.Client).CreateTaskPermission(ctx, repo.CreateTaskPermissionInput{
		TaskID: "t1", Tool: "Read", Granted: false,
	})
	require.NoError(t, err)
	require.NoError(t, bundle1.Close())

	bundle2, err := db.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = bundle2.Close() }()

	var count int
	require.NoError(t, bundle2.DB.QueryRow(`SELECT COUNT(*) FROM grants`).Scan(&count))
	require.Equal(t, 0, count, "an ungranted task_permission must not become a grant")
}
