package db

import (
	"context"
	"database/sql"
	"fmt"
)

// MarkerStore records one-shot events in applied_migrations — the table
// migrateRenameStages introduced. It creates the table itself so it does not
// depend on that migration having run first.
type MarkerStore struct{ DB *sql.DB }

func (m MarkerStore) ensure(ctx context.Context) error {
	_, err := m.DB.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS applied_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`)
	return err
}

func (m MarkerStore) Has(ctx context.Context, name string) (bool, error) {
	if err := m.ensure(ctx); err != nil {
		return false, fmt.Errorf("markers: %w", err)
	}
	var n int
	if err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM applied_migrations WHERE name = ?`, name).Scan(&n); err != nil {
		return false, fmt.Errorf("markers: has %q: %w", name, err)
	}
	return n > 0, nil
}

func (m MarkerStore) Record(ctx context.Context, name string) error {
	if err := m.ensure(ctx); err != nil {
		return fmt.Errorf("markers: %w", err)
	}
	if _, err := m.DB.ExecContext(ctx,
		`INSERT OR IGNORE INTO applied_migrations (name, applied_at) VALUES (?, datetime('now'))`, name); err != nil {
		return fmt.Errorf("markers: record %q: %w", name, err)
	}
	return nil
}
