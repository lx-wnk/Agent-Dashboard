package services_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/services"
)

// openTestDB opens an in-memory database with the schema migrated.
// Local copy because cross-package test helpers do not work in Go.
func openTestDB(t *testing.T) *ent.Client {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	return bundle.Client
}

type resolverFixture struct {
	client     *ent.Client
	resolver   services.SpawnerResolver
	tasks      repo.TaskRepo
	projects   repo.ProjectRepo
	spawners   repo.SpawnerRepo
	defSpawner *ent.Spawner
	altSpawner *ent.Spawner
}

func setupResolver(t *testing.T) *resolverFixture {
	t.Helper()
	client := openTestDB(t)
	taskRepo := repo.NewTaskRepo(client)
	projectRepo := repo.NewProjectRepo(client)
	spawnerRepo := repo.NewSpawnerRepo(client)

	def, err := spawnerRepo.Create(t.Context(), "Claude Default", "claude-default", "claude", nil, nil, nil, nil, "claude", nil, true)
	require.NoError(t, err)

	alt, err := spawnerRepo.Create(t.Context(), "Custom 1", "custom-1", "claude", nil, nil, nil, nil, "claude", nil, false)
	require.NoError(t, err)

	return &resolverFixture{
		client:     client,
		resolver:   services.NewSpawnerResolver(taskRepo, projectRepo, spawnerRepo),
		tasks:      taskRepo,
		projects:   projectRepo,
		spawners:   spawnerRepo,
		defSpawner: def,
		altSpawner: alt,
	}
}

func createTaskWith(t *testing.T, f *resolverFixture, slug string, projectID, spawnerID *string) string {
	t.Helper()
	task, err := f.tasks.Create(t.Context(), repo.CreateTaskInput{
		Slug:                slug,
		Title:               "T",
		Cwd:                 "/tmp",
		CurrentStage:        "concept",
		Priority:            "medium",
		MaxIterations:       20,
		StageTimeoutSeconds: 1800,
		ProjectID:           projectID,
		SpawnerID:           spawnerID,
	})
	require.NoError(t, err)
	return task.ID
}

func TestSpawnerResolver_FallsBackToClaudeDefault(t *testing.T) {
	f := setupResolver(t)

	taskID := createTaskWith(t, f, "bare", nil, nil)

	sp, src, err := f.resolver.Resolve(t.Context(), taskID)
	require.NoError(t, err)
	require.Equal(t, services.SpawnerSourceDefault, src)
	require.Equal(t, f.defSpawner.ID, sp.ID)
}

func TestSpawnerResolver_ProjectDefault(t *testing.T) {
	f := setupResolver(t)

	proj, err := f.projects.Create(t.Context(), "Proj", "proj", nil, nil, &f.altSpawner.ID)
	require.NoError(t, err)

	taskID := createTaskWith(t, f, "with-project", &proj.ID, nil)

	sp, src, err := f.resolver.Resolve(t.Context(), taskID)
	require.NoError(t, err)
	require.Equal(t, services.SpawnerSourceProject, src)
	require.Equal(t, f.altSpawner.ID, sp.ID)
}

func TestSpawnerResolver_TaskWinsOverProject(t *testing.T) {
	f := setupResolver(t)

	// Project sets claude-default as its default; task overrides with alt.
	proj, err := f.projects.Create(t.Context(), "Proj", "proj-override", nil, nil, &f.defSpawner.ID)
	require.NoError(t, err)

	taskID := createTaskWith(t, f, "task-override", &proj.ID, &f.altSpawner.ID)

	sp, src, err := f.resolver.Resolve(t.Context(), taskID)
	require.NoError(t, err)
	require.Equal(t, services.SpawnerSourceTask, src)
	require.Equal(t, f.altSpawner.ID, sp.ID)
}

func TestSpawnerResolver_TaskSpawnerMissingErrors(t *testing.T) {
	f := setupResolver(t)

	bogus := "nonexistent-spawner-id"
	taskID := createTaskWith(t, f, "bad-spawner", nil, &bogus)

	_, _, err := f.resolver.Resolve(t.Context(), taskID)
	require.Error(t, err, "missing task.spawner_id must surface as error, never silent fallback")
}

func TestSpawnerResolver_MissingClaudeDefaultErrors(t *testing.T) {
	f := setupResolver(t)

	// Delete the claude-default spawner to simulate a deployment bug.
	// Delete() refuses BuiltIn so go through ent directly.
	require.NoError(t, f.client.Spawner.DeleteOneID(f.defSpawner.ID).Exec(t.Context()))

	taskID := createTaskWith(t, f, "no-fallback", nil, nil)

	_, _, err := f.resolver.Resolve(t.Context(), taskID)
	require.Error(t, err, "missing claude-default with nothing else set must error (deployment bug)")
}
