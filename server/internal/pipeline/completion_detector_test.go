package pipeline_test

import (
	"testing"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
	"github.com/stretchr/testify/require"
)

func ptr[T any](v T) *T { return &v }

func stageRun(stage string, pid *int, sessionID *string, startedAt *time.Time) *ent.StageRun {
	return &ent.StageRun{Stage: stage, Pid: pid, SessionID: sessionID, StartedAt: startedAt}
}

func TestDetectCompletion_StillRunning(t *testing.T) {
	sr := stageRun("implementation", ptr(1234), nil, ptr(time.Now()))
	deps := pipeline.CompletionDeps{IsPidAlive: func(pid int) bool { return pid == 1234 }}
	result, err := pipeline.DetectCompletion(sr, "/tmp", deps)
	require.NoError(t, err)
	require.Equal(t, "still_running", result.Kind)
}

func TestDetectCompletion_NoSession(t *testing.T) {
	now := time.Now()
	sr := stageRun("implementation", ptr(0), nil, &now)
	deps := pipeline.CompletionDeps{
		IsPidAlive:  func(int) bool { return false },
		FindSession: func(cwd, afterISO string) (string, error) { return "", nil },
	}
	result, err := pipeline.DetectCompletion(sr, "/tmp", deps)
	require.NoError(t, err)
	require.Equal(t, "failed", result.Kind)
}

func TestDetectCompletion_CompletedValid(t *testing.T) {
	now := time.Now()
	sessionID := "abc123"
	sr := stageRun("implementation", ptr(0), &sessionID, &now)
	deps := pipeline.CompletionDeps{
		IsPidAlive: func(int) bool { return false },
		ReadOutput: func(cwd, sid string) (pipeline.StageOutputRead, error) {
			return pipeline.StageOutputRead{
				Output:  map[string]any{"summary": "done", "commits": []any{"abc"}, "openItems": []any{}},
				RawText: "```json\n{}\n```",
			}, nil
		},
	}
	result, err := pipeline.DetectCompletion(sr, "/tmp", deps)
	require.NoError(t, err)
	require.Equal(t, "completed", result.Kind)
}

func TestDetectCompletion_SelfReviewSchemaFail_Retryable(t *testing.T) {
	now := time.Now()
	sessionID := "sid1"
	sr := stageRun("self_review", ptr(0), &sessionID, &now)
	deps := pipeline.CompletionDeps{
		IsPidAlive: func(int) bool { return false },
		ReadOutput: func(cwd, sid string) (pipeline.StageOutputRead, error) {
			return pipeline.StageOutputRead{
				Output:  map[string]any{"summary": "ok"},
				RawText: "```json\n{}\n```",
			}, nil
		},
	}
	result, err := pipeline.DetectCompletion(sr, "/tmp", deps)
	require.NoError(t, err)
	require.Equal(t, "failed", result.Kind)
	require.True(t, result.Retryable)
	require.Contains(t, result.Error, "passed")
}

func TestValidateStageOutput_SelfReview_Valid(t *testing.T) {
	v := pipeline.ValidateStageOutput("self_review", map[string]any{
		"passed":   true,
		"findings": []any{},
		"summary":  "all good",
	})
	require.True(t, v.OK)
}

func TestValidateStageOutput_SelfReview_MissingPassed(t *testing.T) {
	v := pipeline.ValidateStageOutput("self_review", map[string]any{
		"findings": []any{},
		"summary":  "ok",
	})
	require.False(t, v.OK)
	require.Contains(t, v.Error, "passed")
}

func TestValidateStageOutput_Finalization_Valid(t *testing.T) {
	v := pipeline.ValidateStageOutput("finalization", map[string]any{
		"summary":   "all done",
		"insights":  []any{"lesson one"},
		"openTodos": []any{},
		"testPlan":  []any{"run pnpm test"},
	})
	require.True(t, v.OK)
}

func TestValidateStageOutput_Finalization_MissingSummary(t *testing.T) {
	v := pipeline.ValidateStageOutput("finalization", map[string]any{
		"insights":  []any{},
		"openTodos": []any{},
		"testPlan":  []any{},
	})
	require.False(t, v.OK)
	require.Contains(t, v.Error, "summary")
}

func TestValidateStageOutput_Finalization_MissingInsights(t *testing.T) {
	v := pipeline.ValidateStageOutput("finalization", map[string]any{
		"summary":   "done",
		"openTodos": []any{},
		"testPlan":  []any{},
	})
	require.False(t, v.OK)
	require.Contains(t, v.Error, "insights")
}

func TestValidateStageOutput_Finalization_MissingOpenTodos(t *testing.T) {
	v := pipeline.ValidateStageOutput("finalization", map[string]any{
		"summary":  "done",
		"insights": []any{},
		"testPlan": []any{},
	})
	require.False(t, v.OK)
}

func TestValidateStageOutput_Finalization_MissingTestPlan(t *testing.T) {
	v := pipeline.ValidateStageOutput("finalization", map[string]any{
		"summary":   "done",
		"insights":  []any{},
		"openTodos": []any{},
	})
	require.False(t, v.OK)
}

func TestValidateStageOutput_Finalization_WrongType(t *testing.T) {
	// summary as int instead of string → must fail
	v := pipeline.ValidateStageOutput("finalization", map[string]any{
		"summary":   42,
		"insights":  []any{},
		"openTodos": []any{},
		"testPlan":  []any{},
	})
	require.False(t, v.OK)
}
