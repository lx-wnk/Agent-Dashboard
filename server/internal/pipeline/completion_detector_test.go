package pipeline_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
	"github.com/stretchr/testify/require"
)

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

// TestDetectCompletion_ToolOutput_UsedDirectly verifies that when sr.Output is
// populated (e.g. via set_stage_output MCP tool), DetectCompletion returns it
// directly without touching the JSONL scrape path.
func TestDetectCompletion_ToolOutput_UsedDirectly(t *testing.T) {
	sr := &ent.StageRun{
		ID:    "sr-1",
		Stage: "implementation",
		Pid:   ptr(1),
		Output: map[string]any{
			"summary":   "from tool",
			"commits":   []any{},
			"openItems": []any{},
		},
	}
	deps := pipeline.CompletionDeps{
		IsPidAlive: func(int) bool { return false },
		ReadOutput: func(string, string) (pipeline.StageOutputRead, error) {
			t.Fatal("scrape must not run when tool output is present")
			return pipeline.StageOutputRead{}, nil
		},
	}
	res, err := pipeline.DetectCompletion(sr, "/tmp", deps)
	require.NoError(t, err)
	require.Equal(t, "completed", res.Kind)
	require.Equal(t, "from tool", res.Output["summary"])
}

// TestDetectCompletion_SyntheticMarker_NotTreatedAsToolOutput verifies that an
// sr.Output containing "synthetic_session_file" is NOT short-circuited as
// tool output — it must fall through to the synthetic-file handling path.
// With a non-existent path and no session found, the result must be "failed".
func TestDetectCompletion_SyntheticMarker_NotTreatedAsToolOutput(t *testing.T) {
	sr := &ent.StageRun{
		ID:    "sr-2",
		Stage: "implementation",
		Pid:   ptr(1),
		Output: map[string]any{
			"synthetic_session_file": "/nonexistent/x.jsonl",
		},
	}
	now := time.Now()
	sr.StartedAt = &now
	deps := pipeline.CompletionDeps{
		IsPidAlive:  func(int) bool { return false },
		ReadOutput:  func(string, string) (pipeline.StageOutputRead, error) { return pipeline.StageOutputRead{}, nil },
		FindSession: func(string, string) (string, error) { return "", nil },
	}
	res, err := pipeline.DetectCompletion(sr, "/tmp", deps)
	require.NoError(t, err)
	require.Equal(t, "failed", res.Kind)
}

func TestDetectCompletion_InfraAxis(t *testing.T) {
	type tc struct {
		name      string
		sr        func() *ent.StageRun
		deps      pipeline.CompletionDeps
		wantKind  string
		wantInfra bool
		wantRetry bool
	}

	now := time.Now()
	sid := "sid-infra"

	cases := []tc{
		{
			name: "schema_validation_reject_retryable_not_infra",
			sr:   func() *ent.StageRun { return stageRun("self_review", ptr(0), &sid, &now) },
			deps: pipeline.CompletionDeps{
				IsPidAlive: func(int) bool { return false },
				ReadOutput: func(string, string) (pipeline.StageOutputRead, error) {
					return pipeline.StageOutputRead{
						Output:  map[string]any{"summary": "ok"},
						RawText: "```json\n{}\n```",
					}, nil
				},
			},
			wantKind:  "failed",
			wantRetry: true,
			wantInfra: false,
		},
		{
			name: "no_session_jsonl_found_is_infra",
			sr: func() *ent.StageRun {
				return stageRun("implementation", ptr(0), nil, &now)
			},
			deps: pipeline.CompletionDeps{
				IsPidAlive:  func(int) bool { return false },
				FindSession: func(string, string) (string, error) { return "", nil },
			},
			wantKind:  "failed",
			wantInfra: true,
		},
		{
			name: "agent_did_not_produce_json_block_is_infra",
			sr:   func() *ent.StageRun { return stageRun("implementation", ptr(0), &sid, &now) },
			deps: pipeline.CompletionDeps{
				IsPidAlive: func(int) bool { return false },
				ReadOutput: func(string, string) (pipeline.StageOutputRead, error) {
					return pipeline.StageOutputRead{RawText: "some agent output without json block"}, nil
				},
			},
			wantKind:  "failed",
			wantInfra: true,
		},
		{
			name: "session_read_error_is_infra",
			sr:   func() *ent.StageRun { return stageRun("implementation", ptr(0), &sid, &now) },
			deps: pipeline.CompletionDeps{
				IsPidAlive: func(int) bool { return false },
				ReadOutput: func(string, string) (pipeline.StageOutputRead, error) {
					return pipeline.StageOutputRead{}, fmt.Errorf("disk read failed")
				},
			},
			wantKind:  "failed",
			wantInfra: true,
		},
		{
			name: "clean_valid_json_not_infra",
			sr:   func() *ent.StageRun { return stageRun("implementation", ptr(0), &sid, &now) },
			deps: pipeline.CompletionDeps{
				IsPidAlive: func(int) bool { return false },
				ReadOutput: func(string, string) (pipeline.StageOutputRead, error) {
					return pipeline.StageOutputRead{
						Output:  map[string]any{"summary": "done", "commits": []any{}, "openItems": []any{}},
						RawText: "```json\n{}\n```",
					}, nil
				},
			},
			wantKind:  "completed",
			wantInfra: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := pipeline.DetectCompletion(c.sr(), "/tmp", c.deps)
			require.NoError(t, err)
			require.Equal(t, c.wantKind, res.Kind)
			require.Equal(t, c.wantInfra, res.Infra, "Infra mismatch")
			if c.wantRetry {
				require.True(t, res.Retryable)
			}
		})
	}
}

func TestClassifyInfra(t *testing.T) {
	cases := []struct {
		rawText string
		errStr  string
		want    bool
	}{
		{"session limit reached", "", true},
		{"", "overloaded_error occurred", true},
		{"Claude was killed", "", true},
		{"exceeded your quota", "", true},
		{"rate_limit hit", "", true},
		{"usage limit exceeded", "", true},
		{"missing required field: passed", "", false},
		{"", "missing required field: summary", false},
		{"normal output text", "some other error", false},
	}

	for _, c := range cases {
		got := pipeline.ClassifyInfraForTest(c.rawText, c.errStr)
		require.Equal(t, c.want, got, "rawText=%q errStr=%q", c.rawText, c.errStr)
	}
}
