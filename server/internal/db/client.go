package db

import (
	"context"
	"database/sql"
	"fmt"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
)

// Open returns an ent.Client connected to the SQLite database at path.
// Creates the database file if absent. Runs auto-migrate to apply schema.
// Use ":memory:" as path for in-memory databases (testing).
func Open(path string) (*ent.Client, error) {
	// modernc.org/sqlite uses _pragma=<name>(<value>) URI parameters,
	// not the _fk=1 shorthand used by mattn/go-sqlite3.
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)"
	if path == ":memory:" {
		dsn = "file::memory:?mode=memory&_pragma=foreign_keys(1)"
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
	return client, nil
}
