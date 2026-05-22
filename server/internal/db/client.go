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
	// Rename github_login → provider_login before ent auto-migrate so ent finds
	// the column under the new name and does not add a blank provider_login column
	// alongside the old one. Idempotent: skipped when provider_login already exists.
	if err := migrateRenameGithubLogin(sqlDB); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("db: rename github_login: %w", err)
	}
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
	// Always drop and recreate the FTS sync triggers so that schema changes to
	// trigger bodies take effect on existing databases. CREATE TRIGGER IF NOT EXISTS
	// is a no-op when the trigger already exists, so an explicit DROP is required.
	for _, stmt := range []string{
		`DROP TRIGGER IF EXISTS tasks_ai`,
		`DROP TRIGGER IF EXISTS tasks_au`,
		`DROP TRIGGER IF EXISTS tasks_ad`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("raw migration (drop trigger): %w\nstatement: %s", err, stmt)
		}
	}

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
			description
		)`,

		// Sync trigger: INSERT on tasks.
		`CREATE TRIGGER IF NOT EXISTS tasks_ai AFTER INSERT ON tasks BEGIN
			INSERT INTO task_fts(rowid, task_id, title, description)
			VALUES (new.rowid, new.id, new.title, COALESCE(new.description, ''));
		END`,

		// Sync trigger: UPDATE on tasks (delete old index entry, insert new one).
		// task_fts is a regular (content-owning) FTS5 table, so plain DELETE by
		// rowid is required; the "INSERT INTO ft(ft, rowid) VALUES('delete', ...)"
		// form is only valid for contentless FTS5 tables (content='').
		`CREATE TRIGGER IF NOT EXISTS tasks_au AFTER UPDATE ON tasks BEGIN
			DELETE FROM task_fts WHERE rowid = old.rowid;
			INSERT INTO task_fts(rowid, task_id, title, description)
			VALUES (new.rowid, new.id, new.title, COALESCE(new.description, ''));
		END`,

		// Sync trigger: DELETE on tasks.
		`CREATE TRIGGER IF NOT EXISTS tasks_ad AFTER DELETE ON tasks BEGIN
			DELETE FROM task_fts WHERE rowid = old.rowid;
		END`,

		// workflow_patterns: top ngrams discovered from JSONL session files.
		`CREATE TABLE IF NOT EXISTS workflow_patterns (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tools TEXT NOT NULL UNIQUE,
			frequency INTEGER NOT NULL DEFAULT 1,
			last_seen_at TEXT NOT NULL
		)`,

		// api_keys.name no longer requires uniqueness — names are human labels and
		// the token hash (key_hash) is the real unique credential identifier.
		// This index was created by the original ent schema; drop it idempotently
		// so existing databases are migrated without a table rebuild.
		`DROP INDEX IF EXISTS api_keys_name_key`,
	}

	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("raw migration failed: %w\nstatement: %s", err, stmt)
		}
	}
	return nil
}

// migrateRenameGithubLogin renames the users.github_login column to provider_login.
// Runs before ent auto-migrate so ent sees the new name. Idempotent: no-op when
// provider_login already exists (column was already renamed or DB is brand-new).
func migrateRenameGithubLogin(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(users)`)
	if err != nil {
		return fmt.Errorf("PRAGMA table_info: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var hasOld, hasNew bool
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return fmt.Errorf("scan table_info: %w", err)
		}
		switch name {
		case "github_login":
			hasOld = true
		case "provider_login":
			hasNew = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if hasNew || !hasOld {
		return nil // already migrated or users table doesn't exist yet
	}
	_, err = db.Exec(`ALTER TABLE users RENAME COLUMN github_login TO provider_login`)
	return err
}
