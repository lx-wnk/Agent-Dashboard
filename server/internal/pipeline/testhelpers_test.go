package pipeline_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
)

// ptr returns a pointer to v. Used in table-driven tests that need *T fields.
func ptr[T any](v T) *T { return &v }

// stubStageHandler is a test double that returns a predetermined transition.
type stubStageHandler struct {
	stage      string
	transition pipeline.StageTransition
}

func (h *stubStageHandler) Stage() string       { return h.stage }
func (h *stubStageHandler) RequiresAgent() bool { return false }
func (h *stubStageHandler) Execute(_ *pipeline.StageContext) (pipeline.StageTransition, error) {
	return h.transition, nil
}

// makeTestOrchestratorWithRepos opens an in-memory SQLite DB and returns an orchestrator + task repo.
func makeTestOrchestratorWithRepos(t *testing.T) (*pipeline.PipelineOrchestrator, repo.TaskRepo) {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	client := bundle.Client
	t.Cleanup(func() { _ = client.Close() })

	taskRepo := repo.NewTaskRepo(client)
	srRepo := repo.NewStageRunRepo(client)
	permRepo := repo.NewPermissionRepo(client)
	auditRepo := repo.NewAuditEventRepo(client)
	cfgRepo := repo.NewPipelineConfigRepo(client)

	orch, err := pipeline.NewOrchestrator(pipeline.OrchestratorOptions{
		TaskRepo:       taskRepo,
		StageRunRepo:   srRepo,
		PermissionRepo: permRepo,
		AuditRepo:      auditRepo,
		ConfigRepo:     cfgRepo,
	})
	require.NoError(t, err)
	return orch, taskRepo
}

// makeTestOrchestrator is a convenience wrapper for tests that only need the orchestrator.
func makeTestOrchestrator(t *testing.T) *pipeline.PipelineOrchestrator {
	t.Helper()
	orch, _ := makeTestOrchestratorWithRepos(t)
	return orch
}
