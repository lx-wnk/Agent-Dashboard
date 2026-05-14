package repo

import (
	"context"
	"fmt"

	"entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/auditlog"
)

type AuditRepo interface {
	Append(ctx context.Context, input AppendAuditInput) error
	ListForTask(ctx context.Context, taskID string) ([]*ent.AuditLog, error)
	ListAll(ctx context.Context, limit, offset int) ([]*ent.AuditLog, error)
}

type AppendAuditInput struct {
	TaskID  string
	Actor   string
	Action  string
	Details map[string]any
}

type entAuditRepo struct{ client *ent.Client }

func NewAuditRepo(client *ent.Client) AuditRepo {
	return &entAuditRepo{client: client}
}

func (r *entAuditRepo) Append(ctx context.Context, in AppendAuditInput) error {
	q := r.client.AuditLog.Create().
		SetID(uuid.New().String()).
		SetTaskID(in.TaskID).
		SetActor(in.Actor).
		SetAction(in.Action)
	if in.Details != nil {
		q = q.SetDetails(in.Details)
	}
	_, err := q.Save(ctx)
	if err != nil {
		return fmt.Errorf("audit.Append: %w", err)
	}
	return nil
}

func (r *entAuditRepo) ListForTask(ctx context.Context, taskID string) ([]*ent.AuditLog, error) {
	logs, err := r.client.AuditLog.Query().
		Where(auditlog.TaskID(taskID)).
		Order(auditlog.ByTimestamp()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("audit.ListForTask: %w", err)
	}
	return logs, nil
}

func (r *entAuditRepo) ListAll(ctx context.Context, limit, offset int) ([]*ent.AuditLog, error) {
	logs, err := r.client.AuditLog.Query().
		Order(auditlog.ByTimestamp(sql.OrderDesc())).
		Limit(limit).
		Offset(offset).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("audit.ListAll: %w", err)
	}
	return logs, nil
}
