package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	mcp "github.com/lx-wnk/agent-dashboard/server/internal/mcp"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
	"github.com/stretchr/testify/require"
)

func TestRegisterControlTools_AllToolsPresent(t *testing.T) {
	registry := mcp.ToolRegistry{}
	RegisterControlTools(registry, ControlDeps{})
	for _, name := range []string{
		"progress_task",
		"cancel_task",
		"retry_task",
		"grant_permission",
		"resolve_permission_request",
		"approve_all_pending",
	} {
		require.Contains(t, registry, name, "expected tool %q to be registered", name)
	}
}

// TestApproveAllPendingTool_ReturnsCountAndRequeued verifies the MCP tool
// resolves pending requests and returns the correct approved count.
func TestApproveAllPendingTool_ReturnsCountAndRequeued(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })

	client := bundle.Client
	ctx := context.Background()

	taskRepo := repo.NewTaskRepo(client)
	srRepo := repo.NewStageRunRepo(client)
	permRepo := repo.NewPermissionRepo(client)
	auditRepo := repo.NewAuditEventRepo(client)

	// Seed: task → awaiting_user stage run → 1 pending permission request.
	task, err := taskRepo.Create(ctx, repo.CreateTaskInput{
		Slug:          "mcp-approve-test",
		Title:         "MCP Approve Test",
		Cwd:           t.TempDir(),
		MaxIterations: 3,
		Priority:      "normal",
		CurrentStage:  "implementation",
	})
	require.NoError(t, err)

	run, err := srRepo.Create(ctx, repo.CreateStageRunInput{
		TaskID:    task.ID,
		Stage:     "implementation",
		Iteration: 0,
	})
	require.NoError(t, err)
	awaiting := "awaiting_user"
	_, err = srRepo.Update(ctx, run.ID, repo.UpdateStageRunInput{Status: &awaiting})
	require.NoError(t, err)

	pattern := "echo hello"
	_, err = permRepo.CreatePermissionRequest(ctx, repo.CreatePermissionRequestInput{
		StageRunID: run.ID,
		Tool:       "Bash",
		Pattern:    &pattern,
	})
	require.NoError(t, err)

	// Wire MCP tool with a stub orchestrator that captures RequeueForUser.
	requeued := false
	orch := &stubOrchestrator{requeueFn: func(taskID string) {
		if taskID == task.ID {
			requeued = true
		}
	}}

	registry := mcp.ToolRegistry{}
	RegisterControlTools(registry, ControlDeps{
		TaskRepo:     taskRepo,
		SRRepo:       srRepo,
		PermRepo:     permRepo,
		AuditRepo:    auditRepo,
		Orchestrator: orch,
	})

	tool, ok := registry["approve_all_pending"]
	require.True(t, ok)

	result, err := tool.Handler(ctx, map[string]any{"task_id": task.ID})
	require.NoError(t, err)
	require.NotNil(t, result)

	require.NotEmpty(t, result.Content)
	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Content[0].Text), &payload))
	require.EqualValues(t, 1, payload["approved"], "expected approved=1")
	require.True(t, requeued, "expected orchestrator.RequeueForUser called")

	// Confirm the request is resolved.
	runIDs := []string{run.ID}
	stillPending, err := permRepo.ListPendingForTask(ctx, task.ID, runIDs)
	require.NoError(t, err)
	require.Empty(t, stillPending, "expected no pending requests after approve_all_pending")
}

// stubOrchestrator satisfies ControlOrchestrator for MCP tool tests.
type stubOrchestrator struct {
	requeueFn func(taskID string)
}

func (s *stubOrchestrator) ProgressTask(_ context.Context, _ string, _ *pipeline.ProgressOpts) (*ent.StageRun, error) {
	return nil, nil
}
func (s *stubOrchestrator) ResumeFromUser(_ context.Context, _ string) (*ent.StageRun, error) {
	return nil, nil
}
func (s *stubOrchestrator) RequeueForUser(_ context.Context, taskID, _ string) (*ent.StageRun, error) {
	if s.requeueFn != nil {
		s.requeueFn(taskID)
	}
	return &ent.StageRun{ID: taskID + "-stub"}, nil
}
func (s *stubOrchestrator) NotifyTaskTerminated(_ context.Context, _, _ string) {}
