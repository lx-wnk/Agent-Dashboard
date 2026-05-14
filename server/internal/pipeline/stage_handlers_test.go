package pipeline_test

import (
	"context"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
	"github.com/stretchr/testify/require"
)

func TestBacklogHandler_TransitionsToImplementation(t *testing.T) {
	h := pipeline.GetHandlerForStage("backlog")
	require.NotNil(t, h)
	require.False(t, h.RequiresAgent())

	audited := false
	ctx := &pipeline.StageContext{
		Ctx:      context.Background(),
		Task:     &ent.Task{Slug: "my-task", CurrentStage: "backlog"},
		StageRun: &ent.StageRun{Stage: "backlog"},
		RecordAudit: func(action string, _ map[string]any) {
			audited = true
		},
		RequestPermission: func(tool, pattern, reason string) *ent.PermissionRequest { return nil },
	}
	transition, err := h.Execute(ctx)
	require.NoError(t, err)
	next, ok := transition.(pipeline.NextTransition)
	require.True(t, ok)
	require.Equal(t, "implementation", next.Stage)
	require.True(t, audited)
}

func TestConceptHandler_WaitsUser(t *testing.T) {
	h := pipeline.GetHandlerForStage("concept")
	require.NotNil(t, h)
	require.False(t, h.RequiresAgent())

	ctx := &pipeline.StageContext{
		Ctx:               context.Background(),
		Task:              &ent.Task{},
		StageRun:          &ent.StageRun{},
		RecordAudit:       func(string, map[string]any) {},
		RequestPermission: func(string, string, string) *ent.PermissionRequest { return nil },
	}
	transition, err := h.Execute(ctx)
	require.NoError(t, err)
	_, ok := transition.(pipeline.WaitUserTransition)
	require.True(t, ok)
}

func TestBuildFeedbackPrefix_WithValidationError(t *testing.T) {
	prefix := pipeline.BuildFeedbackPrefix(map[string]any{
		"validation_error": "missing field: passed",
		"rejected_output":  map[string]any{"summary": "ok"},
	})
	require.Contains(t, prefix, "CORRECTION REQUIRED")
	require.Contains(t, prefix, "missing field: passed")
}

func TestBuildFeedbackPrefix_NoError(t *testing.T) {
	prefix := pipeline.BuildFeedbackPrefix(nil)
	require.Empty(t, prefix)
}
