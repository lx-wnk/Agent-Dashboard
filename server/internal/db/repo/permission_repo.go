package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/permissionrequest"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/taskpermission"
)

type PermissionRepo interface {
	CreateTaskPermission(ctx context.Context, input CreateTaskPermissionInput) (*ent.TaskPermission, error)
	ListTaskPermissions(ctx context.Context, taskID string) ([]*ent.TaskPermission, error)
	DeleteTaskPermission(ctx context.Context, id string) error
	CreatePermissionRequest(ctx context.Context, input CreatePermissionRequestInput) (*ent.PermissionRequest, error)
	GetPermissionRequest(ctx context.Context, id string) (*ent.PermissionRequest, error)
	ListPendingForStageRun(ctx context.Context, stageRunID string) ([]*ent.PermissionRequest, error)
	ResolvePermissionRequest(ctx context.Context, id, outcome string) (*ent.PermissionRequest, error)
	CountForStageRun(ctx context.Context, stageRunID string) (int, error)
}

type CreateTaskPermissionInput struct {
	TaskID      string
	Tool        string
	Pattern     *string
	Granted     bool
	PreApproved bool
	ExpiresAt   *time.Time
}

type CreatePermissionRequestInput struct {
	StageRunID string
	Tool       string
	Pattern    *string
	Reason     *string
}

type entPermissionRepo struct{ client *ent.Client }

func NewPermissionRepo(client *ent.Client) PermissionRepo {
	return &entPermissionRepo{client: client}
}

func (r *entPermissionRepo) CreateTaskPermission(ctx context.Context, in CreateTaskPermissionInput) (*ent.TaskPermission, error) {
	q := r.client.TaskPermission.Create().
		SetID(uuid.New().String()).
		SetTaskID(in.TaskID).
		SetTool(in.Tool).
		SetGranted(in.Granted).
		SetPreApproved(in.PreApproved)
	if in.Pattern != nil {
		q = q.SetPattern(*in.Pattern)
	}
	if in.ExpiresAt != nil {
		q = q.SetExpiresAt(*in.ExpiresAt)
	}
	p, err := q.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("permission.CreateTaskPermission: %w", err)
	}
	return p, nil
}

func (r *entPermissionRepo) ListTaskPermissions(ctx context.Context, taskID string) ([]*ent.TaskPermission, error) {
	perms, err := r.client.TaskPermission.Query().
		Where(taskpermission.TaskID(taskID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("permission.ListTaskPermissions: %w", err)
	}
	return perms, nil
}

func (r *entPermissionRepo) DeleteTaskPermission(ctx context.Context, id string) error {
	if err := r.client.TaskPermission.DeleteOneID(id).Exec(ctx); err != nil {
		return fmt.Errorf("permission.DeleteTaskPermission: %w", err)
	}
	return nil
}

func (r *entPermissionRepo) CreatePermissionRequest(ctx context.Context, in CreatePermissionRequestInput) (*ent.PermissionRequest, error) {
	q := r.client.PermissionRequest.Create().
		SetID(uuid.New().String()).
		SetStageRunID(in.StageRunID).
		SetTool(in.Tool)
	if in.Pattern != nil {
		q = q.SetPattern(*in.Pattern)
	}
	if in.Reason != nil {
		q = q.SetReason(*in.Reason)
	}
	req, err := q.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("permission.CreatePermissionRequest: %w", err)
	}
	return req, nil
}

func (r *entPermissionRepo) GetPermissionRequest(ctx context.Context, id string) (*ent.PermissionRequest, error) {
	req, err := r.client.PermissionRequest.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("permission.GetPermissionRequest: %w", err)
	}
	return req, nil
}

func (r *entPermissionRepo) ListPendingForStageRun(ctx context.Context, stageRunID string) ([]*ent.PermissionRequest, error) {
	reqs, err := r.client.PermissionRequest.Query().
		Where(permissionrequest.StageRunID(stageRunID), permissionrequest.OutcomeIsNil()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("permission.ListPendingForStageRun: %w", err)
	}
	return reqs, nil
}

func (r *entPermissionRepo) ResolvePermissionRequest(ctx context.Context, id, outcome string) (*ent.PermissionRequest, error) {
	now := time.Now()
	req, err := r.client.PermissionRequest.UpdateOneID(id).
		SetOutcome(outcome).
		SetResolvedAt(now).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("permission.ResolvePermissionRequest: %w", err)
	}
	return req, nil
}

func (r *entPermissionRepo) CountForStageRun(ctx context.Context, stageRunID string) (int, error) {
	n, err := r.client.PermissionRequest.Query().
		Where(permissionrequest.StageRunID(stageRunID), permissionrequest.OutcomeIsNil()).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("permission.CountForStageRun: %w", err)
	}
	return n, nil
}
