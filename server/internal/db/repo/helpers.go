package repo

import (
	"context"
	"errors"
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
// error or panic. This is the shape new transactional code in this package
// should use; adopting it in the repositories that already hand-roll their own
// transaction (a deferred blind rollback in some, an explicit per-branch
// rollback in others) has not happened yet.
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

// Deletion refusals shared by managed resources. Only the spawner repository
// had refusals of this shape; they are shared so every kind can refuse for a
// named reason rather than inventing its own error string.
var (
	// ErrResourceBuiltIn means the resource ships with the dashboard and is not
	// the user's to delete.
	ErrResourceBuiltIn = errors.New("resource is built in and cannot be deleted")
	// ErrResourceReferenced means something else still points at this resource.
	ErrResourceReferenced = errors.New("resource is still referenced")
)
