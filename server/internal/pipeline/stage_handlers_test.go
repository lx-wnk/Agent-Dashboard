package pipeline_test

import (
	"context"
	"strings"
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

func TestBuildStageUserPrompt(t *testing.T) {
	const fullSpec = "## Task Spec\n\nDo the full thing."
	const extraInstruction = "also do this extra thing"

	tests := []struct {
		name             string
		resumeSessionID  string
		priorOutput      map[string]any
		additionalPrompt string
		wantContains     []string
		wantAbsent       []string
	}{
		{
			name:         "fresh run passes full spec through",
			wantContains: []string{fullSpec},
		},
		{
			name:            "resume omits full spec, injects continue instruction",
			resumeSessionID: "sess-abc123",
			wantAbsent:      []string{fullSpec},
			wantContains:    []string{pipeline.ResumeContinueInstructionForTest},
		},
		{
			name:             "user additional prompt appended on fresh run",
			additionalPrompt: extraInstruction,
			wantContains:     []string{fullSpec, extraInstruction},
		},
		{
			name:             "user additional prompt appended on resume",
			resumeSessionID:  "sess-def456",
			additionalPrompt: extraInstruction,
			wantContains:     []string{pipeline.ResumeContinueInstructionForTest, extraInstruction},
			wantAbsent:       []string{fullSpec},
		},
		{
			name: "prior iteration feedback preserved on fresh run",
			priorOutput: map[string]any{
				"validation_error": "missing field: passed",
				"rejected_output":  map[string]any{"x": 1},
			},
			wantContains: []string{"CORRECTION REQUIRED", "missing field: passed", fullSpec},
		},
		{
			name:            "prior iteration feedback preserved on resume",
			resumeSessionID: "sess-ghi789",
			priorOutput: map[string]any{
				"validation_error": "missing field: passed",
				"rejected_output":  map[string]any{"x": 1},
			},
			wantContains: []string{"CORRECTION REQUIRED", "missing field: passed", pipeline.ResumeContinueInstructionForTest},
			wantAbsent:   []string{fullSpec},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := &pipeline.StageContext{
				ResumeSessionID:      tc.resumeSessionID,
				PriorIterationOutput: tc.priorOutput,
				UserAdditionalPrompt: tc.additionalPrompt,
			}
			bundle := pipeline.PromptBundle{UserPrompt: fullSpec}
			result := pipeline.BuildStageUserPromptForTest(ctx, bundle, pipeline.BuildFeedbackPrefix(ctx.PriorIterationOutput))
			for _, want := range tc.wantContains {
				require.Contains(t, result, want)
			}
			for _, absent := range tc.wantAbsent {
				require.NotContains(t, result, absent)
			}
		})
	}
}

// TestPlanReviewHandler_RegisteredAndRequiresAgent verifies the plan_review entry
// in NewStageHandlers is wired as an agent stage.
func TestPlanReviewHandler_RegisteredAndRequiresAgent(t *testing.T) {
	h := pipeline.GetHandlerForStage("plan_review")
	require.NotNil(t, h, "plan_review must be registered in NewStageHandlers")
	require.True(t, h.RequiresAgent(), "plan_review must be an agent-driven stage")
}

// TestPlanReviewBuilder_PromptContainsConceptAndSelfReview verifies that the
// prompt builder injects the task's concept metadata AND a self-review instruction.
func TestPlanReviewBuilder_PromptContainsConceptAndSelfReview(t *testing.T) {
	const conceptSpec = "Build the feature with these steps: A, B, C"
	task := &ent.Task{
		Title:    "My Plan Task",
		PlanMode: true,
		Metadata: map[string]any{"spec": conceptSpec},
	}
	ctx := &pipeline.StageContext{
		Ctx:               context.Background(),
		Task:              task,
		StageRun:          &ent.StageRun{Stage: "plan_review"},
		RecordAudit:       func(string, map[string]any) {},
		RequestPermission: func(string, string, string) *ent.PermissionRequest { return nil },
	}
	bundle := pipeline.PlanReviewBuilderForTest(ctx)
	combined := bundle.SystemPrompt + "\n" + bundle.UserPrompt
	require.Contains(t, combined, conceptSpec, "prompt must embed the concept/spec content")
	require.Truef(t,
		strings.Contains(combined, "self-review") || strings.Contains(combined, "critique") || strings.Contains(combined, "self_review"),
		"prompt must contain a self-review or critique instruction, got:\n%s", combined)
}

// TestPlanReviewBuilder_IncludesFeedbackWhenPresent verifies that reject feedback
// stored under the metadata key is injected into the prompt.
func TestPlanReviewBuilder_IncludesFeedbackWhenPresent(t *testing.T) {
	const feedbackText = "The plan is missing error-handling steps"
	task := &ent.Task{
		Title:    "Feedback Task",
		PlanMode: true,
		Metadata: map[string]any{"planReviewFeedback": feedbackText},
	}
	ctx := &pipeline.StageContext{
		Ctx:               context.Background(),
		Task:              task,
		StageRun:          &ent.StageRun{Stage: "plan_review"},
		RecordAudit:       func(string, map[string]any) {},
		RequestPermission: func(string, string, string) *ent.PermissionRequest { return nil },
	}
	bundle := pipeline.PlanReviewBuilderForTest(ctx)
	combined := bundle.SystemPrompt + "\n" + bundle.UserPrompt
	require.Contains(t, combined, feedbackText, "prompt must embed planReviewFeedback when present")
}

// TestPlanReviewBuilder_NoFeedbackIsDefensive verifies that an absent
// planReviewFeedback key still produces a valid non-empty prompt.
func TestPlanReviewBuilder_NoFeedbackIsDefensive(t *testing.T) {
	task := &ent.Task{
		Title:    "No Feedback Task",
		PlanMode: true,
		Metadata: map[string]any{"spec": "some spec"},
	}
	ctx := &pipeline.StageContext{
		Ctx:               context.Background(),
		Task:              task,
		StageRun:          &ent.StageRun{Stage: "plan_review"},
		RecordAudit:       func(string, map[string]any) {},
		RequestPermission: func(string, string, string) *ent.PermissionRequest { return nil },
	}
	bundle := pipeline.PlanReviewBuilderForTest(ctx)
	require.NotEmpty(t, bundle.UserPrompt, "prompt must be non-empty even without feedback")
}
