package pipeline

import (
	"context"
	"testing"
)

func TestModelResolver_StageDefault_CodedDefaults(t *testing.T) {
	cases := []struct {
		stage string
		want  string
	}{
		{"implementation", defaultModelImplementation},
		{"self_review", defaultModelSelfReview},
		{"plan_review", defaultModelPlanReview},
		{"finalization", defaultModelFinalization},
		{"unknown_stage", ""},
	}

	for _, tc := range cases {
		t.Run(tc.stage, func(t *testing.T) {
			cfgRepo := &fakeConfigRepo{}
			r := newModelResolver(newConfigCache(cfgRepo), cfgRepo)
			ctx := context.Background()

			got := r.StageDefault(ctx, tc.stage, nil)

			if got != tc.want {
				t.Fatalf("got %q, want coded default %q", got, tc.want)
			}
		})
	}
}

func TestModelResolver_StageDefault_GlobalConfigOverridesCoded(t *testing.T) {
	cfgRepo := &fakeConfigRepo{stringValue: "claude-sonnet-4-6"}
	r := newModelResolver(newConfigCache(cfgRepo), cfgRepo)
	ctx := context.Background()

	got := r.StageDefault(ctx, "implementation", nil)

	if got != "claude-sonnet-4-6" {
		t.Fatalf("got %q, want global config override claude-sonnet-4-6", got)
	}
	if cfgRepo.stringCalls != 1 {
		t.Fatalf("cfgRepo.GetString called %d times, want 1", cfgRepo.stringCalls)
	}
}

func TestModelResolver_StageDefault_GlobalLookupIsCached(t *testing.T) {
	cfgRepo := &fakeConfigRepo{stringValue: "claude-sonnet-4-6"}
	cache := newConfigCache(cfgRepo)
	r := newModelResolver(cache, cfgRepo)
	ctx := context.Background()

	r.StageDefault(ctx, "implementation", nil)
	r.StageDefault(ctx, "implementation", nil)

	if cfgRepo.stringCalls != 1 {
		t.Fatalf("cfgRepo.GetString called %d times, want 1 (second read should hit shared cache)", cfgRepo.stringCalls)
	}
}

// projectScopedRepo is a fakeConfigRepo variant that returns a distinct value
// for GetStringScoped, so tests can assert the project-scoped path bypasses
// the cache and calls the repo directly.
type projectScopedRepo struct {
	fakeConfigRepo
	scopedCalls int
	scopedValue string
}

func (p *projectScopedRepo) GetStringScoped(_ context.Context, _ *string, _, fallback string) string {
	p.scopedCalls++
	if p.scopedValue != "" {
		return p.scopedValue
	}
	return fallback
}

func TestModelResolver_StageDefault_ProjectScopedOverridesGlobal(t *testing.T) {
	cfgRepo := &projectScopedRepo{
		fakeConfigRepo: fakeConfigRepo{stringValue: "claude-sonnet-4-6"},
		scopedValue:    "claude-haiku-4-5",
	}
	r := newModelResolver(newConfigCache(cfgRepo), cfgRepo)
	ctx := context.Background()
	projectID := "proj-1"

	got := r.StageDefault(ctx, "implementation", &projectID)

	if got != "claude-haiku-4-5" {
		t.Fatalf("got %q, want project-scoped override claude-haiku-4-5", got)
	}
	if cfgRepo.scopedCalls != 1 {
		t.Fatalf("cfgRepo.GetStringScoped called %d times, want 1", cfgRepo.scopedCalls)
	}
	if cfgRepo.stringCalls != 0 {
		t.Fatalf("cfgRepo.GetString called %d times, want 0 (project path must not use the global cache)", cfgRepo.stringCalls)
	}
}

func TestModelResolver_StageDefault_ProjectScopedFallsBackToCodedDefault(t *testing.T) {
	cfgRepo := &projectScopedRepo{}
	r := newModelResolver(newConfigCache(cfgRepo), cfgRepo)
	ctx := context.Background()
	projectID := "proj-1"

	got := r.StageDefault(ctx, "finalization", &projectID)

	if got != defaultModelFinalization {
		t.Fatalf("got %q, want coded default %q via GetStringScoped fallback", got, defaultModelFinalization)
	}
}
