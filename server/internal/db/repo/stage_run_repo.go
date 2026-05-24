package repo

import (
	"context"
	databasesql "database/sql"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/stagerun"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/rawrepo"
)

type StageRunRepo interface {
	Create(ctx context.Context, input CreateStageRunInput) (*ent.StageRun, error)
	GetByID(ctx context.Context, id string) (*ent.StageRun, error)
	GetBySessionID(ctx context.Context, sessionID string) (*ent.StageRun, error)
	GetLatestForTask(ctx context.Context, taskID string) (*ent.StageRun, error)
	GetLatestByTaskAndStage(ctx context.Context, taskID, stage string) (*ent.StageRun, error)
	GetByTaskStageIteration(ctx context.Context, taskID, stage string, iteration int) (*ent.StageRun, error)
	ListForTask(ctx context.Context, taskID string) ([]*ent.StageRun, error)
	// ListStageRunsByTaskIDs returns all stage_runs for the given task IDs in a
	// single bulk query, grouped by task_id. Eliminates N+1 queries in export routes.
	ListStageRunsByTaskIDs(ctx context.Context, taskIDs []string) (map[string][]*ent.StageRun, error)
	ListByStatus(ctx context.Context, statuses ...string) ([]*ent.StageRun, error)
	ListPending(ctx context.Context) ([]*ent.StageRun, error)
	Update(ctx context.Context, id string, input UpdateStageRunInput) (*ent.StageRun, error)
	SumCompletedCostCents(ctx context.Context, taskID string) (int64, error)
	// GetLatestForTasks returns the most-recent stage_run per task using a
	// ROW_NUMBER() window function — correctness is exact regardless of iteration
	// count, unlike the former Go-side heuristic limit of len(ids)*20+20.
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

type entStageRunRepo struct {
	client   *ent.Client
	bulkRepo rawrepo.StageRunBulkRepo // nil when created without a *sql.DB (e.g. TX context)
}

// NewStageRunRepo returns a StageRunRepo backed by client only (no raw-SQL
// bulk helpers). Use NewStageRunRepoWithDB for the full implementation that
// includes window-function-based GetLatestForTasks and ListStageRunsByTaskIDs.
func NewStageRunRepo(client *ent.Client) StageRunRepo {
	return &entStageRunRepo{client: client}
}

// NewStageRunRepoWithDB returns a StageRunRepo that also has access to the
// underlying *sql.DB for window-function bulk queries. Use this in the DI
// composition root instead of NewStageRunRepo where a DBBundle is available.
func NewStageRunRepoWithDB(client *ent.Client, db *databasesql.DB) StageRunRepo {
	return &entStageRunRepo{client: client, bulkRepo: rawrepo.NewStageRunBulkRepo(db)}
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

func (r *entStageRunRepo) SumCompletedCostCents(ctx context.Context, taskID string) (int64, error) {
	type aggResult struct {
		Sum int `json:"sum"`
	}
	var result []aggResult
	err := r.client.StageRun.Query().
		Where(stagerun.TaskID(taskID), stagerun.StatusIn("done", "failed")).
		Aggregate(ent.Sum(stagerun.FieldCostCents)).
		Scan(ctx, &result)
	if err != nil {
		return 0, fmt.Errorf("stagerun.SumCompletedCostCents: %w", err)
	}
	if len(result) == 0 {
		return 0, nil
	}
	return int64(result[0].Sum), nil
}

// GetLatestForTasks returns the most-recent stage_run per task using a
// ROW_NUMBER() window function when a *sql.DB is available (production path).
// Falls back to the ent-based heuristic limit when bulkRepo is nil (transaction
// context or test without DB injection).
func (r *entStageRunRepo) GetLatestForTasks(ctx context.Context, taskIDs []string) (map[string]*ent.StageRun, error) {
	if r.bulkRepo != nil {
		result, err := r.bulkRepo.LatestPerTask(ctx, taskIDs)
		if err != nil {
			return nil, fmt.Errorf("stagerun.GetLatestForTasks: %w", err)
		}
		return result, nil
	}
	// Fallback: ent-based query used in transaction contexts (txSR) where
	// bulkRepo is unavailable. The heuristic limit is acceptable there because
	// transaction-scoped GetLatestForTasks is only called for single-task lookups.
	limit := len(taskIDs)*20 + 20
	runs, err := r.client.StageRun.Query().
		Where(stagerun.TaskIDIn(taskIDs...)).
		Order(stagerun.ByCreatedAt(sql.OrderDesc())).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("stagerun.GetLatestForTasks: %w", err)
	}
	result := make(map[string]*ent.StageRun, len(taskIDs))
	for _, run := range runs {
		if _, exists := result[run.TaskID]; !exists {
			result[run.TaskID] = run
		}
	}
	return result, nil
}

// ListStageRunsByTaskIDs returns all stage_runs for the given task IDs in a
// single bulk query, grouped by task_id. This eliminates the N+1 pattern in
// the export route where ListForTask was called once per task.
func (r *entStageRunRepo) ListStageRunsByTaskIDs(ctx context.Context, taskIDs []string) (map[string][]*ent.StageRun, error) {
	if r.bulkRepo != nil {
		result, err := r.bulkRepo.AllForTaskIDs(ctx, taskIDs)
		if err != nil {
			return nil, fmt.Errorf("stagerun.ListStageRunsByTaskIDs: %w", err)
		}
		return result, nil
	}
	// Fallback: ent-based query used when bulkRepo is unavailable.
	if len(taskIDs) == 0 {
		return map[string][]*ent.StageRun{}, nil
	}
	runs, err := r.client.StageRun.Query().
		Where(stagerun.TaskIDIn(taskIDs...)).
		Order(stagerun.ByIteration()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("stagerun.ListStageRunsByTaskIDs: %w", err)
	}
	result := make(map[string][]*ent.StageRun, len(taskIDs))
	for _, run := range runs {
		result[run.TaskID] = append(result[run.TaskID], run)
	}
	return result, nil
}
