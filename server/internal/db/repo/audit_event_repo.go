package repo

import (
	"context"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/auditevent"
)

// AuditAction is the set of valid security-relevant action strings.
const (
	AuditActionSpawn             = "spawn"
	AuditActionSpawnRejected     = "spawn_rejected"
	AuditActionPermissionGrant   = "permission_grant"
	AuditActionPermissionRevoke  = "permission_revoke"
	AuditActionKeyCreate         = "key_create"
	AuditActionKeyDelete         = "key_delete"
)

// AuditEventRepo persists and queries security-relevant audit events.
type AuditEventRepo interface {
	// RecordAudit writes a single audit event. userID may be nil for system actions.
	RecordAudit(ctx context.Context, userID *string, action, target string, metadata map[string]any) error
	// List returns audit events matching the given filters, newest-first.
	List(ctx context.Context, f AuditEventFilters) ([]*ent.AuditEvent, error)
}

// AuditEventFilters controls which events are returned by List.
type AuditEventFilters struct {
	UserID *string
	Action *string
	Since  *time.Time
	Limit  int // 0 → default 100
	Offset int
}

type entAuditEventRepo struct{ client *ent.Client }

// NewAuditEventRepo creates an AuditEventRepo backed by the given ent client.
func NewAuditEventRepo(client *ent.Client) AuditEventRepo {
	return &entAuditEventRepo{client: client}
}

func (r *entAuditEventRepo) RecordAudit(ctx context.Context, userID *string, action, target string, metadata map[string]any) error {
	q := r.client.AuditEvent.Create().
		SetID(uuid.New().String()).
		SetAction(action).
		SetTarget(target).
		SetNillableUserID(userID)
	if metadata != nil {
		q = q.SetMetadata(metadata)
	}
	if _, err := q.Save(ctx); err != nil {
		return fmt.Errorf("audit_event.RecordAudit: %w", err)
	}
	return nil
}

func (r *entAuditEventRepo) List(ctx context.Context, f AuditEventFilters) ([]*ent.AuditEvent, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}

	q := r.client.AuditEvent.Query()

	if f.UserID != nil {
		q = q.Where(auditevent.UserID(*f.UserID))
	}
	if f.Action != nil {
		q = q.Where(auditevent.Action(*f.Action))
	}
	if f.Since != nil {
		q = q.Where(auditevent.TsGTE(*f.Since))
	}

	events, err := q.
		Order(auditevent.ByTs(sql.OrderDesc())).
		Limit(limit).
		Offset(f.Offset).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("audit_event.List: %w", err)
	}
	return events, nil
}
