package pipeline_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/kontor/server/internal/db"
	"github.com/lx-wnk/kontor/server/internal/db/ent"
	"github.com/lx-wnk/kontor/server/internal/db/repo"
	"github.com/lx-wnk/kontor/server/internal/pipeline"
)

// makeFinalizationOrchestrator builds an orchestrator with the given
// OrchestratorOptions overrides applied on top of the minimal repo set every
// orchestrator requires.
func makeFinalizationOrchestrator(t *testing.T, configure func(*pipeline.OrchestratorOptions)) *pipeline.PipelineOrchestrator {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })

	opts := pipeline.OrchestratorOptions{
		TaskRepo:       repo.NewTaskRepo(bundle.Client),
		StageRunRepo:   repo.NewStageRunRepo(bundle.Client),
		PermissionRepo: repo.NewPermissionRepo(bundle.Client),
		AuditRepo:      repo.NewAuditEventRepo(bundle.Client),
		ConfigRepo:     repo.NewPipelineConfigRepo(bundle.Client),
	}
	if configure != nil {
		configure(&opts)
	}
	orch, err := pipeline.NewOrchestrator(opts)
	require.NoError(t, err)
	return orch
}

func worktreeTask() *ent.Task {
	return &ent.Task{
		ID:           "task-1",
		Slug:         "my-task",
		Title:        "add foo",
		WorktreePath: ptr("/tmp/wt-my-task"),
		SourceBranch: ptr("feat/my-task"),
	}
}

func finalizationRun() *ent.StageRun {
	return &ent.StageRun{ID: "run-1", Stage: "finalization"}
}

func failIfCalled(t *testing.T, name string) {
	t.Helper()
	t.Fatalf("%s must not be called", name)
}

func TestDecideFinalization_CleanPushed_CreatesPR(t *testing.T) {
	ctx := context.Background()
	orch := makeFinalizationOrchestrator(t, func(o *pipeline.OrchestratorOptions) {
		o.HasUnpushedWorkFn = func(ctx context.Context, task *ent.Task) bool { return false }
		o.CreateDraftPRFn = func(ctx context.Context, worktreePath, branch, base, title, prBody string) (int, string, error) {
			return 42, "https://github.com/lx-wnk/kontor/pull/42", nil
		}
	})

	transition := orch.DecideCompletedTransitionForTest(ctx, worktreeTask(), finalizationRun(), map[string]any{"summary": "done"})

	done, ok := transition.(pipeline.DoneTransition)
	require.True(t, ok, "expected DoneTransition, got %T", transition)
	require.Equal(t, 42, done.MetadataPatch["pr_number"])
	require.Equal(t, "https://github.com/lx-wnk/kontor/pull/42", done.MetadataPatch["pr_url"])
}

func TestDecideFinalization_Dirty_Fails(t *testing.T) {
	ctx := context.Background()
	orch := makeFinalizationOrchestrator(t, func(o *pipeline.OrchestratorOptions) {
		o.HasUnpushedWorkFn = func(ctx context.Context, task *ent.Task) bool { return true }
		o.CreateDraftPRFn = func(ctx context.Context, worktreePath, branch, base, title, prBody string) (int, string, error) {
			failIfCalled(t, "CreateDraftPRFn")
			return 0, "", nil
		}
	})

	transition := orch.DecideCompletedTransitionForTest(ctx, worktreeTask(), finalizationRun(), map[string]any{})

	fail, ok := transition.(pipeline.FailTransition)
	require.True(t, ok, "expected FailTransition, got %T", transition)
	require.Contains(t, fail.Reason, "unpushed")
}

func TestDecideFinalization_UnpushedPushSucceeds_CreatesPR(t *testing.T) {
	ctx := context.Background()
	pushCalled := false
	orch := makeFinalizationOrchestrator(t, func(o *pipeline.OrchestratorOptions) {
		o.AllowGitPush = true
		o.PushFn = func(ctx context.Context, task *ent.Task) error {
			pushCalled = true
			return nil
		}
		o.HasUnpushedWorkFn = func(ctx context.Context, task *ent.Task) bool { return false }
		o.CreateDraftPRFn = func(ctx context.Context, worktreePath, branch, base, title, prBody string) (int, string, error) {
			return 7, "https://github.com/lx-wnk/kontor/pull/7", nil
		}
	})

	transition := orch.DecideCompletedTransitionForTest(ctx, worktreeTask(), finalizationRun(), map[string]any{})

	require.True(t, pushCalled, "PushFn must be called when push is allowed")
	done, ok := transition.(pipeline.DoneTransition)
	require.True(t, ok, "expected DoneTransition, got %T", transition)
	require.Equal(t, 7, done.MetadataPatch["pr_number"])
}

func TestDecideFinalization_UnpushedPushFails(t *testing.T) {
	ctx := context.Background()
	orch := makeFinalizationOrchestrator(t, func(o *pipeline.OrchestratorOptions) {
		o.AllowGitPush = true
		o.PushFn = func(ctx context.Context, task *ent.Task) error {
			return errors.New("remote rejected")
		}
		o.CreateDraftPRFn = func(ctx context.Context, worktreePath, branch, base, title, prBody string) (int, string, error) {
			failIfCalled(t, "CreateDraftPRFn")
			return 0, "", nil
		}
	})

	transition := orch.DecideCompletedTransitionForTest(ctx, worktreeTask(), finalizationRun(), map[string]any{})

	fail, ok := transition.(pipeline.FailTransition)
	require.True(t, ok, "expected FailTransition, got %T", transition)
	require.Contains(t, fail.Reason, "git push failed")
	require.Contains(t, fail.Reason, "remote rejected")
}

func TestDecideFinalization_UnpushedPushNotAllowed(t *testing.T) {
	ctx := context.Background()
	orch := makeFinalizationOrchestrator(t, func(o *pipeline.OrchestratorOptions) {
		o.AllowGitPush = false
		o.PushFn = func(ctx context.Context, task *ent.Task) error {
			failIfCalled(t, "PushFn")
			return nil
		}
		o.HasUnpushedWorkFn = func(ctx context.Context, task *ent.Task) bool { return true }
	})

	transition := orch.DecideCompletedTransitionForTest(ctx, worktreeTask(), finalizationRun(), map[string]any{})

	fail, ok := transition.(pipeline.FailTransition)
	require.True(t, ok, "expected FailTransition, got %T", transition)
	require.Contains(t, fail.Reason, "unpushed")
	require.Contains(t, fail.Reason, "disabled")
}

func TestDecideFinalization_ExistingPRIdempotent(t *testing.T) {
	ctx := context.Background()
	calls := 0
	orch := makeFinalizationOrchestrator(t, func(o *pipeline.OrchestratorOptions) {
		o.HasUnpushedWorkFn = func(ctx context.Context, task *ent.Task) bool { return false }
		o.CreateDraftPRFn = func(ctx context.Context, worktreePath, branch, base, title, prBody string) (int, string, error) {
			calls++
			return 9, "https://github.com/lx-wnk/kontor/pull/9", nil
		}
	})

	first := orch.DecideCompletedTransitionForTest(ctx, worktreeTask(), finalizationRun(), map[string]any{})
	second := orch.DecideCompletedTransitionForTest(ctx, worktreeTask(), finalizationRun(), map[string]any{})

	for _, transition := range []pipeline.StageTransition{first, second} {
		done, ok := transition.(pipeline.DoneTransition)
		require.True(t, ok, "expected DoneTransition, got %T", transition)
		require.Equal(t, 9, done.MetadataPatch["pr_number"])
	}
	// The pipeline always calls CreateDraftPRFn on each finalization; idempotency
	// lives inside ProductionCreateDraftPRFn (findExistingPR), not the pipeline itself.
	require.Equal(t, 2, calls, "pipeline delegates both calls; ProductionCreateDraftPRFn owns the idempotency guard")
}

func TestDecideFinalization_NoWorktree_Passthrough(t *testing.T) {
	ctx := context.Background()
	orch := makeFinalizationOrchestrator(t, func(o *pipeline.OrchestratorOptions) {
		o.HasUnpushedWorkFn = func(ctx context.Context, task *ent.Task) bool {
			failIfCalled(t, "HasUnpushedWorkFn")
			return false
		}
		o.PushFn = func(ctx context.Context, task *ent.Task) error {
			failIfCalled(t, "PushFn")
			return nil
		}
		o.CreateDraftPRFn = func(ctx context.Context, worktreePath, branch, base, title, prBody string) (int, string, error) {
			failIfCalled(t, "CreateDraftPRFn")
			return 0, "", nil
		}
	})

	task := &ent.Task{ID: "task-2", Slug: "no-worktree-task", Title: "add bar"}
	transition := orch.DecideCompletedTransitionForTest(ctx, task, finalizationRun(), map[string]any{"summary": "done"})

	done, ok := transition.(pipeline.DoneTransition)
	require.True(t, ok, "expected DoneTransition, got %T", transition)
	require.Nil(t, done.MetadataPatch)
}

func TestResolveBase_NeverMainMaster(t *testing.T) {
	tests := []struct {
		name   string
		target *string
		want   string
	}{
		{"main falls back", ptr("main"), "develop"},
		{"master falls back", ptr("master"), "develop"},
		{"unset falls back", nil, "develop"},
		{"custom target kept", ptr("staging"), "staging"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			task := &ent.Task{TargetBranch: tc.target}
			got := pipeline.ResolveBaseForTest(task)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestBuildPRBody_Format(t *testing.T) {
	task := &ent.Task{ID: "task-3", Slug: "my-task", Title: "add foo"}
	finOutput := map[string]any{
		"summary":   "Implemented foo end to end.",
		"testPlan":  []any{"run pnpm test", "open the app and click foo"},
		"openTodos": []any{"wire the retry button"},
	}
	selfReviewOutput := map[string]any{
		"findings": []any{
			map[string]any{"severity": "high", "description": "no input validation", "file": "foo.go"},
			map[string]any{"severity": "low", "description": "minor naming nit", "file": "foo.go"},
		},
	}

	body := pipeline.BuildPRBodyForTest(task, finOutput, selfReviewOutput)

	require.Contains(t, body, "- [ ] run pnpm test")
	require.Contains(t, body, "- [ ] open the app and click foo")
	require.Contains(t, body, "wire the retry button")
	require.Contains(t, body, "[high] no input validation (foo.go)")
	require.NotContains(t, body, "minor naming nit", "low-severity findings must not appear in Known Issues")
	require.Contains(t, body, "Kontor task: `my-task`")
	require.Contains(t, body, "Generated with")
}

// TestProductionCreateDraftPRFn_ReusesExistingPR exercises the findExistingPR
// code path inside ProductionCreateDraftPRFn using a PATH-injected fake gh
// binary. The fake returns an existing open PR for pr list and fails for any
// other subcommand, so the test asserts that pr create is never reached.
func TestProductionCreateDraftPRFn_ReusesExistingPR(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake sh script not supported on windows")
	}
	dir := t.TempDir()
	script := `#!/bin/sh
if [ "$1" = "pr" ] && [ "$2" = "list" ]; then
  echo '[{"number":7,"url":"https://github.com/lx-wnk/kontor/pull/7"}]'
  exit 0
fi
echo "unexpected gh call: $*" >&2
exit 1
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0700))
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	number, url, err := pipeline.ProductionCreateDraftPRFn(
		context.Background(), dir, "feat/my-task", "develop", "feat: my task", "body",
	)
	require.NoError(t, err)
	require.Equal(t, 7, number)
	require.Equal(t, "https://github.com/lx-wnk/kontor/pull/7", url)
}

func TestDeriveConventionalTitle(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{"already prefixed feat", "feat: add foo", "feat: add foo"},
		{"already prefixed with scope", "fix(api): handle nil", "fix(api): handle nil"},
		{"unprefixed gets feat", "add foo", "feat: add foo"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			task := &ent.Task{Title: tc.title}
			require.Equal(t, tc.want, pipeline.DeriveConventionalTitleForTest(task))
		})
	}
}
