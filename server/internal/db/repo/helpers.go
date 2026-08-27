package repo

import (
	"context"
	"fmt"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
)

// IsNotFound reports whether err is ent's not-found error. It exists so callers
// outside this package do not have to import ent just to classify an error.
func IsNotFound(err error) bool { return ent.IsNotFound(err) }

// rollback aborts tx and returns cause, preserving the original failure when the
// rollback itself also fails.
func rollback(tx *ent.Tx, cause error) error {
	if rerr := tx.Rollback(); rerr != nil {
		return fmt.Errorf("%w (rollback failed: %v)", cause, rerr)
	}
	return cause
}

// WithTx runs fn inside a transaction, committing on success and rolling back on
// error or panic. It replaces the two rollback idioms this package grew: a
// deferred blind rollback in some repos and an explicit per-branch rollback in
// others.
func WithTx(ctx context.Context, client *ent.Client, fn func(tx *ent.Tx) error) error {
	tx, err := client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()
	if err := fn(tx); err != nil {
		return rollback(tx, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}
