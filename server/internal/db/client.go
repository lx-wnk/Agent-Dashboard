package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
)

// DBBundle holds both the ent client and the underlying *sql.DB.
// The raw *sql.DB is needed for repositories that execute hand-written SQL
// (e.g. FTS5 queries, notification_config, push_subscriptions).
// Calling Close() or Client.Close() invalidates both fields.
type DBBundle struct {
	Client *ent.Client
	DB     *sql.DB
}

// Close closes the database connection. Both Client and DB become invalid after this call.
// Note: Client.Close() also closes DB because the ent driver wraps the same *sql.DB.
func (b *DBBundle) Close() error {
	return b.Client.Close()
}

// Open returns a DBBundle connected to the SQLite database at path.
// Creates the database file and any missing parent directories if absent.
// Runs ent auto-migrate followed by runRawMigrations for hand-written tables.
// Use ":memory:" as path for in-memory databases (testing).
func Open(path string) (*DBBundle, error) {
	// modernc.org/sqlite uses _pragma=<name>(<value>) URI parameters,
	// not the _fk=1 shorthand used by mattn/go-sqlite3.
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_pragma=cache_size(-64000)&_pragma=temp_store(memory)&_pragma=mmap_size(134217728)"
	if path == ":memory:" {
		dsn = "file::memory:?mode=memory&_pragma=foreign_keys(1)"
	} else {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return nil, fmt.Errorf("db: create parent dirs for %q: %w", path, err)
		}
	}
	// modernc.org/sqlite registers as "sqlite"; ent's dialect constant is "sqlite3".
	// Use OpenDB with the ent dialect constant so ent recognises the SQLite dialect.
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("db: open %q: %w", path, err)
	}
	drv := entsql.OpenDB(dialect.SQLite, sqlDB)
	client := ent.NewClient(ent.Driver(drv))
	if err := client.Schema.Create(context.Background()); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("db: auto-migrate: %w", err)
	}
	if err := runRawMigrations(sqlDB); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("db: raw migrations: %w", err)
	}
	return &DBBundle{Client: client, DB: sqlDB}, nil
}

// runRawMigrations creates tables and FTS5 virtual tables that are not managed
// by ent. Each statement is executed individually — SQLite does not support
// multi-statement Exec calls.
func runRawMigrations(db *sql.DB) error {
	stmts := []string{
		// notification_config: key-value store for VAPID keys etc.
		`CREATE TABLE IF NOT EXISTS notification_config (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,

		// push_subscriptions: Web Push browser subscriptions.
		`CREATE TABLE IF NOT EXISTS push_subscriptions (
			id TEXT PRIMARY KEY,
			endpoint TEXT UNIQUE NOT NULL,
			p256dh TEXT NOT NULL,
			auth TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,

		// FTS5 virtual table for full-text search over tasks.
		`CREATE VIRTUAL TABLE IF NOT EXISTS task_fts USING fts5(
			task_id UNINDEXED,
			title,
			description,
			content='tasks',
			content_rowid='rowid'
		)`,

		// Sync trigger: INSERT on tasks.
		`CREATE TRIGGER IF NOT EXISTS tasks_ai AFTER INSERT ON tasks BEGIN
			INSERT INTO task_fts(rowid, task_id, title, description)
			VALUES (new.rowid, new.id, new.title, COALESCE(new.description, ''));
		END`,

		// Sync trigger: UPDATE on tasks (delete old row, insert new row).
		`CREATE TRIGGER IF NOT EXISTS tasks_au AFTER UPDATE ON tasks BEGIN
			INSERT INTO task_fts(task_fts, rowid, task_id, title, description)
			VALUES ('delete', old.rowid, old.id, old.title, COALESCE(old.description, ''));
			INSERT INTO task_fts(rowid, task_id, title, description)
			VALUES (new.rowid, new.id, new.title, COALESCE(new.description, ''));
		END`,

		// Sync trigger: DELETE on tasks.
		`CREATE TRIGGER IF NOT EXISTS tasks_ad AFTER DELETE ON tasks BEGIN
			INSERT INTO task_fts(task_fts, rowid) VALUES ('delete', old.rowid);
		END`,

		// workflow_patterns: top ngrams discovered from JSONL session files.
		`CREATE TABLE IF NOT EXISTS workflow_patterns (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tools TEXT NOT NULL UNIQUE,
			frequency INTEGER NOT NULL DEFAULT 1,
			last_seen_at TEXT NOT NULL
		)`,
	}

	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("raw migration failed: %w\nstatement: %s", err, stmt)
		}
	}
	return nil
}
