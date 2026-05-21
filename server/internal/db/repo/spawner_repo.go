package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/project"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/spawner"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/task"
)

// ErrSpawnerBuiltIn is returned when attempting to delete a built-in spawner.
var ErrSpawnerBuiltIn = errors.New("cannot delete a built-in spawner")

// ErrSpawnerInUse is returned when attempting to delete a spawner that is still
// referenced by at least one Task or Project.
var ErrSpawnerInUse = errors.New("spawner is still referenced by one or more tasks or projects")

// SpawnerRepo manages spawner persistence.
type SpawnerRepo interface {
	Create(ctx context.Context, name, slug, command string, args []string, env map[string]string, modelOverride, description *string, builtIn bool) (*ent.Spawner, error)
	GetByID(ctx context.Context, id string) (*ent.Spawner, error)
	GetBySlug(ctx context.Context, slug string) (*ent.Spawner, error)
	List(ctx context.Context) ([]*ent.Spawner, error)
	Update(ctx context.Context, id string, name, slug, command *string, args []string, env map[string]string, modelOverride, description *string, clearModelOverride, clearDescription bool) (*ent.Spawner, error)
	Delete(ctx context.Context, id string) error
}

type entSpawnerRepo struct {
	client *ent.Client
}

// NewSpawnerRepo returns a SpawnerRepo backed by the given ent client.
func NewSpawnerRepo(client *ent.Client) SpawnerRepo {
	return &entSpawnerRepo{client: client}
}

func (r *entSpawnerRepo) Create(ctx context.Context, name, slug, command string, args []string, env map[string]string, modelOverride, description *string, builtIn bool) (*ent.Spawner, error) {
	if args == nil {
		args = []string{}
	}
	if env == nil {
		env = map[string]string{}
	}
	s, err := r.client.Spawner.Create().
		SetID(uuid.New().String()).
		SetName(name).
		SetSlug(slug).
		SetCommand(command).
		SetArgs(args).
		SetEnv(env).
		SetNillableModelOverride(modelOverride).
		SetNillableDescription(description).
		SetBuiltIn(builtIn).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("spawner.Create: %w", err)
	}
	return s, nil
}

func (r *entSpawnerRepo) GetByID(ctx context.Context, id string) (*ent.Spawner, error) {
	s, err := r.client.Spawner.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("spawner.GetByID: %w", err)
	}
	return s, nil
}

func (r *entSpawnerRepo) GetBySlug(ctx context.Context, slug string) (*ent.Spawner, error) {
	s, err := r.client.Spawner.Query().
		Where(spawner.Slug(slug)).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("spawner.GetBySlug: %w", err)
	}
	return s, nil
}

func (r *entSpawnerRepo) List(ctx context.Context) ([]*ent.Spawner, error) {
	spawners, err := r.client.Spawner.Query().
		Order(spawner.ByCreatedAt()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("spawner.List: %w", err)
	}
	return spawners, nil
}

func (r *entSpawnerRepo) Update(ctx context.Context, id string, name, slug, command *string, args []string, env map[string]string, modelOverride, description *string, clearModelOverride, clearDescription bool) (*ent.Spawner, error) {
	q := r.client.Spawner.UpdateOneID(id).SetUpdatedAt(time.Now())
	if name != nil {
		q = q.SetName(*name)
	}
	if slug != nil {
		q = q.SetSlug(*slug)
	}
	if command != nil {
		q = q.SetCommand(*command)
	}
	if args != nil {
		q = q.SetArgs(args)
	}
	if env != nil {
		q = q.SetEnv(env)
	}
	if clearModelOverride {
		q = q.ClearModelOverride()
	} else if modelOverride != nil {
		q = q.SetModelOverride(*modelOverride)
	}
	if clearDescription {
		q = q.ClearDescription()
	} else if description != nil {
		q = q.SetDescription(*description)
	}
	s, err := q.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("spawner.Update: %w", err)
	}
	return s, nil
}

// Delete removes a spawner. Returns ErrSpawnerBuiltIn if the spawner is built-in,
// or ErrSpawnerInUse if any Task or Project still references it.
func (r *entSpawnerRepo) Delete(ctx context.Context, id string) error {
	s, err := r.client.Spawner.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("spawner.Delete: %w", err)
	}
	if s.BuiltIn {
		return ErrSpawnerBuiltIn
	}

	taskCount, err := r.client.Task.Query().
		Where(task.SpawnerID(id)).
		Count(ctx)
	if err != nil {
		return fmt.Errorf("spawner.Delete: count task references: %w", err)
	}
	if taskCount > 0 {
		return ErrSpawnerInUse
	}

	projectCount, err := r.client.Project.Query().
		Where(project.DefaultSpawnerID(id)).
		Count(ctx)
	if err != nil {
		return fmt.Errorf("spawner.Delete: count project references: %w", err)
	}
	if projectCount > 0 {
		return ErrSpawnerInUse
	}

	if err := r.client.Spawner.DeleteOneID(id).Exec(ctx); err != nil {
		return fmt.Errorf("spawner.Delete: %w", err)
	}
	return nil
}
