package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/taskschedule"
)

// TaskScheduleRepo persists recurring task-schedule definitions.
type TaskScheduleRepo interface {
	Create(ctx context.Context, in CreateTaskScheduleInput) (*ent.TaskSchedule, error)
	GetByID(ctx context.Context, id string) (*ent.TaskSchedule, error)
	Update(ctx context.Context, id string, in UpdateTaskScheduleInput) (*ent.TaskSchedule, error)
	Delete(ctx context.Context, id string) error
	ListForUser(ctx context.Context, userID string, isAdmin bool) ([]*ent.TaskSchedule, error)
	ListEnabled(ctx context.Context) ([]*ent.TaskSchedule, error)
	ListDue(ctx context.Context, now time.Time) ([]*ent.TaskSchedule, error)
	SetEnabled(ctx context.Context, id string, enabled bool) (*ent.TaskSchedule, error)
	UpdateFireState(ctx context.Context, id string, in FireStateInput) (*ent.TaskSchedule, error)
}

// CreateTaskScheduleInput carries the schedule definition plus its task template.
type CreateTaskScheduleInput struct {
	Name               string
	Enabled            *bool
	NLText             *string
	CronExpr           string
	Timezone           string
	Catchup            string
	SlugPrefix         string
	Title              string
	Description        *string
	Cwd                string
	SourceBranch       *string
	TargetBranch       *string
	Priority           string
	CurrentStage       string
	MaxIterations      int
	TokenBudget        *int
	CostBudgetCents    *int
	StageTimeoutSeconds int
	SilverBullet       bool
	ProjectID          *string
	SpawnerID          *string
	PermissionTemplate *string
	Metadata           map[string]any
	UserID             *string
	NextRunAt          *time.Time
}

// UpdateTaskScheduleInput patches schedule fields. Nil pointers leave a field
// unchanged. Template fields and the cron definition can all be edited.
type UpdateTaskScheduleInput struct {
	Name               *string
	Enabled            *bool
	NLText             *string
	CronExpr           *string
	Timezone           *string
	Catchup            *string
	SlugPrefix         *string
	Title              *string
	Description        *string
	Cwd                *string
	SourceBranch       *string
	TargetBranch       *string
	Priority           *string
	MaxIterations      *int
	TokenBudget        *int
	CostBudgetCents    *int
	StageTimeoutSeconds *int
	SilverBullet       *bool
	ProjectID          *string
	SpawnerID          *string
	PermissionTemplate *string
	Metadata           map[string]any
	NextRunAt          *time.Time
}

// FireStateInput records the result of a fire: the spawned task and the next
// scheduled run. LastRunAt is set to the fire time.
type FireStateInput struct {
	LastRunAt  time.Time
	LastTaskID *string
	NextRunAt  *time.Time
}

type entTaskScheduleRepo struct{ client *ent.Client }

func NewTaskScheduleRepo(client *ent.Client) TaskScheduleRepo {
	return &entTaskScheduleRepo{client: client}
}

func (r *entTaskScheduleRepo) Create(ctx context.Context, in CreateTaskScheduleInput) (*ent.TaskSchedule, error) {
	q := r.client.TaskSchedule.Create().
		SetID(uuid.New().String()).
		SetName(in.Name).
		SetCronExpr(in.CronExpr).
		SetSlugPrefix(in.SlugPrefix).
		SetTitle(in.Title).
		SetCwd(in.Cwd).
		SetMaxIterations(in.MaxIterations).
		SetStageTimeoutSeconds(in.StageTimeoutSeconds).
		SetSilverBullet(in.SilverBullet)

	if in.Enabled != nil {
		q = q.SetEnabled(*in.Enabled)
	}
	if in.Timezone != "" {
		q = q.SetTimezone(in.Timezone)
	}
	if in.Catchup != "" {
		q = q.SetCatchup(in.Catchup)
	}
	if in.Priority != "" {
		q = q.SetPriority(in.Priority)
	}
	if in.CurrentStage != "" {
		q = q.SetCurrentStage(in.CurrentStage)
	}
	q = q.SetNillableNlText(in.NLText).
		SetNillableDescription(in.Description).
		SetNillableSourceBranch(in.SourceBranch).
		SetNillableTargetBranch(in.TargetBranch).
		SetNillableTokenBudget(in.TokenBudget).
		SetNillableCostBudgetCents(in.CostBudgetCents).
		SetNillableProjectID(in.ProjectID).
		SetNillableSpawnerID(in.SpawnerID).
		SetNillablePermissionTemplate(in.PermissionTemplate).
		SetNillableUserID(in.UserID).
		SetNillableNextRunAt(in.NextRunAt)
	if in.Metadata != nil {
		q = q.SetMetadata(in.Metadata)
	}

	s, err := q.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("taskschedule.Create: %w", err)
	}
	return s, nil
}

func (r *entTaskScheduleRepo) GetByID(ctx context.Context, id string) (*ent.TaskSchedule, error) {
	s, err := r.client.TaskSchedule.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("taskschedule.GetByID: %w", err)
	}
	return s, nil
}

func (r *entTaskScheduleRepo) Update(ctx context.Context, id string, in UpdateTaskScheduleInput) (*ent.TaskSchedule, error) {
	q := r.client.TaskSchedule.UpdateOneID(id).SetUpdatedAt(time.Now())
	if in.Name != nil {
		q = q.SetName(*in.Name)
	}
	if in.Enabled != nil {
		q = q.SetEnabled(*in.Enabled)
	}
	if in.CronExpr != nil {
		q = q.SetCronExpr(*in.CronExpr)
	}
	if in.Timezone != nil {
		q = q.SetTimezone(*in.Timezone)
	}
	if in.Catchup != nil {
		q = q.SetCatchup(*in.Catchup)
	}
	if in.SlugPrefix != nil {
		q = q.SetSlugPrefix(*in.SlugPrefix)
	}
	if in.Title != nil {
		q = q.SetTitle(*in.Title)
	}
	if in.Cwd != nil {
		q = q.SetCwd(*in.Cwd)
	}
	if in.Priority != nil {
		q = q.SetPriority(*in.Priority)
	}
	if in.MaxIterations != nil {
		q = q.SetMaxIterations(*in.MaxIterations)
	}
	if in.StageTimeoutSeconds != nil {
		q = q.SetStageTimeoutSeconds(*in.StageTimeoutSeconds)
	}
	if in.SilverBullet != nil {
		q = q.SetSilverBullet(*in.SilverBullet)
	}
	// Nillable pointer fields: a non-nil pointer sets the value. Clearing is not
	// exposed here (absent = unchanged) to mirror the task repo's update contract.
	q = q.SetNillableNlText(in.NLText).
		SetNillableDescription(in.Description).
		SetNillableSourceBranch(in.SourceBranch).
		SetNillableTargetBranch(in.TargetBranch).
		SetNillableTokenBudget(in.TokenBudget).
		SetNillableCostBudgetCents(in.CostBudgetCents).
		SetNillableProjectID(in.ProjectID).
		SetNillableSpawnerID(in.SpawnerID).
		SetNillablePermissionTemplate(in.PermissionTemplate).
		SetNillableNextRunAt(in.NextRunAt)
	if in.Metadata != nil {
		q = q.SetMetadata(in.Metadata)
	}
	s, err := q.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("taskschedule.Update: %w", err)
	}
	return s, nil
}

func (r *entTaskScheduleRepo) Delete(ctx context.Context, id string) error {
	if err := r.client.TaskSchedule.DeleteOneID(id).Exec(ctx); err != nil {
		return fmt.Errorf("taskschedule.Delete: %w", err)
	}
	return nil
}

func (r *entTaskScheduleRepo) ListForUser(ctx context.Context, userID string, isAdmin bool) ([]*ent.TaskSchedule, error) {
	q := r.client.TaskSchedule.Query().Order(ent.Asc(taskschedule.FieldCreatedAt))
	if !isAdmin {
		q = q.Where(taskschedule.UserID(userID))
	}
	rows, err := q.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("taskschedule.ListForUser: %w", err)
	}
	return rows, nil
}

func (r *entTaskScheduleRepo) ListEnabled(ctx context.Context) ([]*ent.TaskSchedule, error) {
	rows, err := r.client.TaskSchedule.Query().Where(taskschedule.Enabled(true)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("taskschedule.ListEnabled: %w", err)
	}
	return rows, nil
}

// ListDue returns enabled schedules whose next_run_at is non-null and at or
// before now. Schedules with a null next_run_at are excluded — the scheduler
// initializes next_run_at on first observation rather than firing immediately.
func (r *entTaskScheduleRepo) ListDue(ctx context.Context, now time.Time) ([]*ent.TaskSchedule, error) {
	rows, err := r.client.TaskSchedule.Query().
		Where(
			taskschedule.Enabled(true),
			taskschedule.NextRunAtNotNil(),
			taskschedule.NextRunAtLTE(now),
		).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("taskschedule.ListDue: %w", err)
	}
	return rows, nil
}

func (r *entTaskScheduleRepo) SetEnabled(ctx context.Context, id string, enabled bool) (*ent.TaskSchedule, error) {
	s, err := r.client.TaskSchedule.UpdateOneID(id).SetEnabled(enabled).SetUpdatedAt(time.Now()).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("taskschedule.SetEnabled: %w", err)
	}
	return s, nil
}

func (r *entTaskScheduleRepo) UpdateFireState(ctx context.Context, id string, in FireStateInput) (*ent.TaskSchedule, error) {
	q := r.client.TaskSchedule.UpdateOneID(id).
		SetLastRunAt(in.LastRunAt).
		SetUpdatedAt(time.Now()).
		SetNillableLastTaskID(in.LastTaskID).
		SetNillableNextRunAt(in.NextRunAt)
	s, err := q.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("taskschedule.UpdateFireState: %w", err)
	}
	return s, nil
}
