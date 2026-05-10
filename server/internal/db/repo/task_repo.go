package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/task"
)

type TaskRepo interface {
	Create(ctx context.Context, input CreateTaskInput) (*ent.Task, error)
	GetByID(ctx context.Context, id string) (*ent.Task, error)
	GetBySlug(ctx context.Context, slug string) (*ent.Task, error)
	Update(ctx context.Context, id string, input UpdateTaskInput) (*ent.Task, error)
	Delete(ctx context.Context, id string) error
	ListForUser(ctx context.Context, userID string, isAdmin bool) ([]*ent.Task, error)
	ListPickable(ctx context.Context) ([]*ent.Task, error)
	ListByStage(ctx context.Context, stage string) ([]*ent.Task, error)
}

type CreateTaskInput struct {
	ID                  string
	Slug                string
	Title               string
	Description         *string
	Cwd                 string
	WorktreePath        *string
	SourceBranch        *string
	TargetBranch        *string
	ParentTaskID        *string
	UserID              *string
	MaxIterations       int
	TokenBudget         *int
	CostBudgetCents     *int
	StageTimeoutSeconds int
	SilverBullet        bool
	Priority            string
	CurrentStage        string
	Metadata            map[string]any
}

type UpdateTaskInput struct {
	Title               *string
	Description         *string
	CurrentStage        *string
	Priority            *string
	SilverBullet        *bool
	MaxIterations       *int
	TokenBudget         *int
	CostBudgetCents     *int
	StageTimeoutSeconds *int
	Metadata            map[string]any
	MetadataClear       bool
}

type entTaskRepo struct{ client *ent.Client }

func NewTaskRepo(client *ent.Client) TaskRepo {
	return &entTaskRepo{client: client}
}

func (r *entTaskRepo) Create(ctx context.Context, in CreateTaskInput) (*ent.Task, error) {
	id := in.ID
	if id == "" {
		id = uuid.New().String()
	}
	q := r.client.Task.Create().
		SetID(id).
		SetSlug(in.Slug).
		SetTitle(in.Title).
		SetCwd(in.Cwd).
		SetCurrentStage(in.CurrentStage).
		SetPriority(in.Priority).
		SetMaxIterations(in.MaxIterations).
		SetStageTimeoutSeconds(in.StageTimeoutSeconds).
		SetSilverBullet(in.SilverBullet)
	if in.Description != nil {
		q = q.SetDescription(*in.Description)
	}
	if in.WorktreePath != nil {
		q = q.SetWorktreePath(*in.WorktreePath)
	}
	if in.SourceBranch != nil {
		q = q.SetSourceBranch(*in.SourceBranch)
	}
	if in.TargetBranch != nil {
		q = q.SetTargetBranch(*in.TargetBranch)
	}
	if in.ParentTaskID != nil {
		q = q.SetParentTaskID(*in.ParentTaskID)
	}
	if in.UserID != nil {
		q = q.SetUserID(*in.UserID)
	}
	if in.TokenBudget != nil {
		q = q.SetTokenBudget(*in.TokenBudget)
	}
	if in.CostBudgetCents != nil {
		q = q.SetCostBudgetCents(*in.CostBudgetCents)
	}
	if in.Metadata != nil {
		q = q.SetMetadata(in.Metadata)
	}
	t, err := q.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("task.Create: %w", err)
	}
	return t, nil
}

func (r *entTaskRepo) GetByID(ctx context.Context, id string) (*ent.Task, error) {
	t, err := r.client.Task.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("task.GetByID: %w", err)
	}
	return t, nil
}

func (r *entTaskRepo) GetBySlug(ctx context.Context, slug string) (*ent.Task, error) {
	t, err := r.client.Task.Query().Where(task.Slug(slug)).Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("task.GetBySlug: %w", err)
	}
	return t, nil
}

func (r *entTaskRepo) Update(ctx context.Context, id string, in UpdateTaskInput) (*ent.Task, error) {
	q := r.client.Task.UpdateOneID(id).SetUpdatedAt(time.Now())
	if in.Title != nil {
		q = q.SetTitle(*in.Title)
	}
	if in.Description != nil {
		q = q.SetDescription(*in.Description)
	}
	if in.CurrentStage != nil {
		q = q.SetCurrentStage(*in.CurrentStage)
	}
	if in.Priority != nil {
		q = q.SetPriority(*in.Priority)
	}
	if in.SilverBullet != nil {
		q = q.SetSilverBullet(*in.SilverBullet)
	}
	if in.MaxIterations != nil {
		q = q.SetMaxIterations(*in.MaxIterations)
	}
	if in.StageTimeoutSeconds != nil {
		q = q.SetStageTimeoutSeconds(*in.StageTimeoutSeconds)
	}
	if in.TokenBudget != nil {
		q = q.SetTokenBudget(*in.TokenBudget)
	}
	if in.CostBudgetCents != nil {
		q = q.SetCostBudgetCents(*in.CostBudgetCents)
	}
	if in.MetadataClear {
		q = q.ClearMetadata()
	} else if in.Metadata != nil {
		q = q.SetMetadata(in.Metadata)
	}
	t, err := q.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("task.Update: %w", err)
	}
	return t, nil
}

func (r *entTaskRepo) Delete(ctx context.Context, id string) error {
	if err := r.client.Task.DeleteOneID(id).Exec(ctx); err != nil {
		return fmt.Errorf("task.Delete: %w", err)
	}
	return nil
}

func (r *entTaskRepo) ListForUser(ctx context.Context, userID string, isAdmin bool) ([]*ent.Task, error) {
	q := r.client.Task.Query().Order(task.ByCreatedAt())
	if !isAdmin {
		q = q.Where(task.UserID(userID))
	}
	tasks, err := q.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("task.ListForUser: %w", err)
	}
	return tasks, nil
}

func (r *entTaskRepo) ListPickable(ctx context.Context) ([]*ent.Task, error) {
	tasks, err := r.client.Task.Query().
		Where(
			task.CurrentStageNotIn("done", "cancelled", "on_hold", "concept"),
		).
		Order(task.BySilverBullet(), task.ByCreatedAt()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("task.ListPickable: %w", err)
	}
	return tasks, nil
}

func (r *entTaskRepo) ListByStage(ctx context.Context, stage string) ([]*ent.Task, error) {
	tasks, err := r.client.Task.Query().Where(task.CurrentStage(stage)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("task.ListByStage: %w", err)
	}
	return tasks, nil
}
