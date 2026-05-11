package repo

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	entdep "github.com/lx-wnk/agent-dashboard/server/internal/db/ent/taskdependency"
)

// DependencyRepo manages task dependency records.
type DependencyRepo interface {
	Add(ctx context.Context, taskID, dependsOnID, requiredStage, onCancelAction string) (*ent.TaskDependency, error)
	Remove(ctx context.Context, taskID, dependsOnID string) (bool, error)
}

type entDependencyRepo struct{ client *ent.Client }

// NewDependencyRepo returns a DependencyRepo backed by the given ent client.
func NewDependencyRepo(client *ent.Client) DependencyRepo {
	return &entDependencyRepo{client: client}
}

func (r *entDependencyRepo) Add(ctx context.Context, taskID, dependsOnID, requiredStage, onCancelAction string) (*ent.TaskDependency, error) {
	dep, err := r.client.TaskDependency.Create().
		SetID(uuid.New().String()).
		SetTaskID(taskID).
		SetDependsOnID(dependsOnID).
		SetRequiredStage(requiredStage).
		SetOnCancelAction(onCancelAction).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("dependency.Add: %w", err)
	}
	return dep, nil
}

func (r *entDependencyRepo) Remove(ctx context.Context, taskID, dependsOnID string) (bool, error) {
	n, err := r.client.TaskDependency.Delete().
		Where(entdep.TaskID(taskID), entdep.DependsOnID(dependsOnID)).
		Exec(ctx)
	if err != nil {
		return false, fmt.Errorf("dependency.Remove: %w", err)
	}
	return n > 0, nil
}
