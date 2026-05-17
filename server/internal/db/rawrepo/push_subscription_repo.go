package rawrepo

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// PushSubscription represents a browser Web Push subscription.
type PushSubscription struct {
	ID        string
	Endpoint  string
	P256dh    string
	Auth      string
	CreatedAt string
}

// PushSubscriptionRepo persists browser push subscriptions.
type PushSubscriptionRepo interface {
	// Register inserts a subscription; silently ignores duplicate endpoints (INSERT OR IGNORE).
	Register(ctx context.Context, sub PushSubscription) error
	// ListAll returns all stored subscriptions.
	ListAll(ctx context.Context) ([]PushSubscription, error)
	// DeleteByEndpoint removes a subscription by its push endpoint URL.
	DeleteByEndpoint(ctx context.Context, endpoint string) error
}

type sqlPushSubscriptionRepo struct{ db *sql.DB }

// NewPushSubscriptionRepo returns a PushSubscriptionRepo backed by db.
func NewPushSubscriptionRepo(db *sql.DB) PushSubscriptionRepo {
	return &sqlPushSubscriptionRepo{db: db}
}

func (r *sqlPushSubscriptionRepo) Register(ctx context.Context, sub PushSubscription) error {
	if sub.ID == "" {
		sub.ID = uuid.New().String()
	}
	if sub.CreatedAt == "" {
		sub.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO push_subscriptions (id, endpoint, p256dh, auth, created_at) VALUES (?, ?, ?, ?, ?)`,
		sub.ID, sub.Endpoint, sub.P256dh, sub.Auth, sub.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("push_subscriptions.Register: %w", err)
	}
	return nil
}

func (r *sqlPushSubscriptionRepo) ListAll(ctx context.Context) ([]PushSubscription, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, endpoint, p256dh, auth, created_at FROM push_subscriptions`,
	)
	if err != nil {
		return nil, fmt.Errorf("push_subscriptions.ListAll: %w", err)
	}
	defer rows.Close()

	var subs []PushSubscription
	for rows.Next() {
		var s PushSubscription
		if err := rows.Scan(&s.ID, &s.Endpoint, &s.P256dh, &s.Auth, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("push_subscriptions.ListAll scan: %w", err)
		}
		subs = append(subs, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("push_subscriptions.ListAll rows: %w", err)
	}
	return subs, nil
}

func (r *sqlPushSubscriptionRepo) DeleteByEndpoint(ctx context.Context, endpoint string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM push_subscriptions WHERE endpoint = ?`,
		endpoint,
	)
	if err != nil {
		return fmt.Errorf("push_subscriptions.DeleteByEndpoint: %w", err)
	}
	return nil
}
