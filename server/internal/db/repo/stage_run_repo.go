package repo

import (
	"context"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/stagerun"
)

type StageRunRepo interface {
	Create(ctx context.Context, input CreateStageRunInput) (*ent.StageRun, error)
	GetByID(ctx context.Context, id string) (*ent.StageRun, error)
	GetBySessionID(ctx context.Context, sessionID string) (*ent.StageRun, error)
	GetLatestForTask(ctx context.Context, taskID string) (*ent.StageRun, error)
	GetLatestByTaskAndStage(ctx context.Context, taskID, stage string) (*ent.StageRun, error)
	GetByTaskStageIteration(ctx context.Context, taskID, stage string, iteration int) (*ent.StageRun, error)
	ListForTask(ctx context.Context, taskID string) ([]*ent.StageRun, error)
	ListByStatus(ctx context.Context, statuses ...string) ([]*ent.StageRun, error)
	ListPending(ctx context.Context) ([]*ent.StageRun, error)
	Update(ctx context.Context, id string, input UpdateStageRunInput) (*ent.StageRun, error)
	SumCompletedCostCents(ctx context.Context, taskID string) (int, error)
	GetLatestForTasks(ctx context.Context, taskIDs []string) (map[string]*ent.StageRun, error)
}

type CreateStageRunInput struct {
	TaskID      string
	Stage       string
	Iteration   int
	SessionName string
}

type UpdateStageRunInput struct {
	Status      *string
	PID         *int
	PIDClear    bool
	SessionID   *string
	Output      map[string]any
	TokensUsed  *int
	CostCents   *int
	StartedAt   *time.Time
	EndedAt     *time.Time
	LastGrantAt *time.Time
}

type entStageRunRepo struct{ client *ent.Client }

func NewStageRunRepo(client *ent.Client) StageRunRepo {
	return &entStageRunRepo{client: client}
}

func (r *entStageRunRepo) Create(ctx context.Context, in CreateStageRunInput) (*ent.StageRun, error) {
	q := r.client.StageRun.Create().
		SetID(uuid.New().String()).
		SetTaskID(in.TaskID).
		SetStage(in.Stage).
		SetIteration(in.Iteration).
		SetStatus("pending")
	if in.SessionName != "" {
		q = q.SetSessionName(in.SessionName)
	}
	sr, err := q.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("stagerun.Create: %w", err)
	}
	return sr, nil
}

func (r *entStageRunRepo) GetByID(ctx context.Context, id string) (*ent.StageRun, error) {
	sr, err := r.client.StageRun.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("stagerun.GetByID: %w", err)
	}
	return sr, nil
}

func (r *entStageRunRepo) GetBySessionID(ctx context.Context, sessionID string) (*ent.StageRun, error) {
	sr, err := r.client.StageRun.Query().
		Where(stagerun.SessionID(sessionID)).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("stagerun.GetBySessionID: %w", err)
	}
	return sr, nil
}

func (r *entStageRunRepo) GetLatestForTask(ctx context.Context, taskID string) (*ent.StageRun, error) {
	sr, err := r.client.StageRun.Query().
		Where(stagerun.TaskID(taskID)).
		Order(stagerun.ByCreatedAt(sql.OrderDesc())).
		First(ctx)
	if err != nil {
		return nil, fmt.Errorf("stagerun.GetLatestForTask: %w", err)
	}
	return sr, nil
}

func (r *entStageRunRepo) GetLatestByTaskAndStage(ctx context.Context, taskID, stage string) (*ent.StageRun, error) {
	sr, err := r.client.StageRun.Query().
		Where(stagerun.TaskID(taskID), stagerun.Stage(stage)).
		Order(stagerun.ByIteration(sql.OrderDesc())).
		First(ctx)
	if err != nil {
		return nil, fmt.Errorf("stagerun.GetLatestByTaskAndStage: %w", err)
	}
	return sr, nil
}

func (r *entStageRunRepo) GetByTaskStageIteration(ctx context.Context, taskID, stage string, iteration int) (*ent.StageRun, error) {
	sr, err := r.client.StageRun.Query().
		Where(stagerun.TaskID(taskID), stagerun.Stage(stage), stagerun.Iteration(iteration)).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("stagerun.GetByTaskStageIteration: %w", err)
	}
	return sr, nil
}

func (r *entStageRunRepo) ListForTask(ctx context.Context, taskID string) ([]*ent.StageRun, error) {
	runs, err := r.client.StageRun.Query().
		Where(stagerun.TaskID(taskID)).
		Order(stagerun.ByIteration()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("stagerun.ListForTask: %w", err)
	}
	return runs, nil
}

func (r *entStageRunRepo) ListByStatus(ctx context.Context, statuses ...string) ([]*ent.StageRun, error) {
	runs, err := r.client.StageRun.Query().
		Where(stagerun.StatusIn(statuses...)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("stagerun.ListByStatus: %w", err)
	}
	return runs, nil
}

func (r *entStageRunRepo) ListPending(ctx context.Context) ([]*ent.StageRun, error) {
	return r.ListByStatus(ctx, "pending")
}

func (r *entStageRunRepo) Update(ctx context.Context, id string, in UpdateStageRunInput) (*ent.StageRun, error) {
	q := r.client.StageRun.UpdateOneID(id)
	if in.Status != nil {
		q = q.SetStatus(*in.Status)
	}
	if in.PIDClear {
		q = q.ClearPid()
	} else if in.PID != nil {
		q = q.SetPid(*in.PID)
	}
	if in.SessionID != nil {
		q = q.SetSessionID(*in.SessionID)
	}
	if in.Output != nil {
		q = q.SetOutput(in.Output)
	}
	if in.TokensUsed != nil {
		q = q.SetTokensUsed(*in.TokensUsed)
	}
	if in.CostCents != nil {
		q = q.SetCostCents(*in.CostCents)
	}
	if in.StartedAt != nil {
		q = q.SetStartedAt(*in.StartedAt)
	}
	if in.EndedAt != nil {
		q = q.SetEndedAt(*in.EndedAt)
	}
	if in.LastGrantAt != nil {
		q = q.SetLastGrantAt(*in.LastGrantAt)
	}
	sr, err := q.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("stagerun.Update: %w", err)
	}
	return sr, nil
}

func (r *entStageRunRepo) SumCompletedCostCents(ctx context.Context, taskID string) (int, error) {
	runs, err := r.client.StageRun.Query().
		Where(stagerun.TaskID(taskID), stagerun.StatusIn("done", "failed")).
		All(ctx)
	if err != nil {
		return 0, fmt.Errorf("stagerun.SumCompletedCostCents: %w", err)
	}
	total := 0
	for _, run := range runs {
		total += run.CostCents
	}
	return total, nil
}

func (r *entStageRunRepo) GetLatestForTasks(ctx context.Context, taskIDs []string) (map[string]*ent.StageRun, error) {
	runs, err := r.client.StageRun.Query().
		Where(stagerun.TaskIDIn(taskIDs...)).
		Order(stagerun.ByCreatedAt()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("stagerun.GetLatestForTasks: %w", err)
	}
	result := make(map[string]*ent.StageRun)
	for _, run := range runs {
		result[run.TaskID] = run // last write wins (latest due to ordering)
	}
	return result, nil
}
