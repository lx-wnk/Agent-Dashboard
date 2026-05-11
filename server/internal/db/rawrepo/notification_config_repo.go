// Package rawrepo contains hand-written SQL repositories for tables not
// managed by ent (notification_config, push_subscriptions, etc.).
package rawrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// NotificationConfigRepo provides key-value access to the notification_config table.
type NotificationConfigRepo interface {
	// Get returns (value, true, nil) when the key exists, ("", false, nil) when absent.
	Get(ctx context.Context, key string) (string, bool, error)
	// Set upserts the key-value pair.
	Set(ctx context.Context, key, value string) error
}

type sqlNotificationConfigRepo struct{ db *sql.DB }

// NewNotificationConfigRepo returns a NotificationConfigRepo backed by db.
func NewNotificationConfigRepo(db *sql.DB) NotificationConfigRepo {
	return &sqlNotificationConfigRepo{db: db}
}

func (r *sqlNotificationConfigRepo) Get(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := r.db.QueryRowContext(ctx,
		`SELECT value FROM notification_config WHERE key = ?`, key,
	).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("notification_config.Get: %w", err)
	}
	return value, true, nil
}

func (r *sqlNotificationConfigRepo) Set(ctx context.Context, key, value string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO notification_config (key, value) VALUES (?, ?)`, key, value,
	)
	if err != nil {
		return fmt.Errorf("notification_config.Set: %w", err)
	}
	return nil
}
