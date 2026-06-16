package tasks_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/tasks"
	"github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

// captureOrchestrator records the ProgressOpts passed to ProgressTask so the
// retry test can assert how the resume session id was resolved.
type captureOrchestrator struct{ opts *pipeline.ProgressOpts }

func (c *captureOrchestrator) ProgressTask(_ context.Context, taskID string, opts *pipeline.ProgressOpts) (*ent.StageRun, error) {
	c.opts = opts
	return &ent.StageRun{ID: taskID + "-run"}, nil
}
func (c *captureOrchestrator) ResumeFromUser(_ context.Context, _ string) (*ent.StageRun, error) {
	return nil, nil
}
func (c *captureOrchestrator) NotifyTaskTerminated(_ context.Context, _, _ string)          {}
func (c *captureOrchestrator) InvalidateConfigCache()                                       {}
func (c *captureOrchestrator) ClearStalePendingPermissions(_ context.Context, _ string)    {}

func (c *captureOrchestrator) resumeID() string {
	if c.opts == nil {
		return ""
	}
	return c.opts.ResumeSessionID
}

// seedFailedRun creates a task at the given stage with one failed stage_run.
// When sessionID is non-empty it is stamped onto the run; when writeFile is
// true the matching session JSONL is created on disk under cwd's project dir.
func seedFailedRun(t *testing.T, client *ent.Client, cwd, stage, sessionID string, writeFile bool) string {
	t.Helper()
	ctx := context.Background()
	taskRepo := repo.NewTaskRepo(client)
	srRepo := repo.NewStageRunRepo(client)

	task, err := taskRepo.Create(ctx, repo.CreateTaskInput{
		Slug:          "retry-resume",
		Title:         "Retry Resume",
		Cwd:           cwd,
		MaxIterations: 5,
		Priority:      "normal",
		CurrentStage:  stage,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	run, err := srRepo.Create(ctx, repo.CreateStageRunInput{TaskID: task.ID, Stage: stage, Iteration: 0})
	if err != nil {
		t.Fatalf("create stage run: %v", err)
	}
	failed := "failed"
	upd := repo.UpdateStageRunInput{Status: &failed}
	if sessionID != "" {
		sid := sessionID
		upd.SessionID = &sid
	}
	if _, err := srRepo.Update(ctx, run.ID, upd); err != nil {
		t.Fatalf("update stage run: %v", err)
	}

	if writeFile && sessionID != "" {
		projectDir, derr := pipeline.ResolvedProjectDir(cwd)
		if derr != nil {
			t.Fatalf("resolve project dir: %v", derr)
		}
		if err := os.MkdirAll(projectDir, 0o700); err != nil {
			t.Fatalf("mkdir project: %v", err)
		}
		p := filepath.Join(projectDir, sessionID+".jsonl")
		if err := os.WriteFile(p, []byte("{}\n"), 0o600); err != nil {
			t.Fatalf("write session: %v", err)
		}
		_ = os.Chtimes(p, time.Now(), time.Now())
	}
	return task.ID
}

func newRetryHandler(t *testing.T, orch tasks.OrchestratorIface) (*ent.Client, *chi.Mux) {
	t.Helper()
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	client := bundle.Client
	t.Cleanup(func() { _ = client.Close() })

	h := tasks.NewHandler(tasks.Deps{
		TaskRepo:     repo.NewTaskRepo(client),
		SRRepo:       repo.NewStageRunRepo(client),
		PermRepo:     repo.NewPermissionRepo(client),
		AuditRepo:    repo.NewAuditEventRepo(client),
		CfgRepo:      repo.NewPipelineConfigRepo(client),
		Orchestrator: orch,
		Broadcaster:  sse.NewTaskBroadcaster(sse.NewBroadcaster()),
	})
	r := chi.NewRouter()
	r.Use(auth.RequireAuth(testJWTSecret))
	h.Mount(r)
	return client, r
}

func postRetry(t *testing.T, r *chi.Mux, taskID string) *httptest.ResponseRecorder {
	t.Helper()
	req := withAuth(t, httptest.NewRequest(http.MethodPost, "/api/tasks/"+taskID+"/retry", nil))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestRetry_ResumesWhenSessionFileExists(t *testing.T) {
	cwd := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	orch := &captureOrchestrator{}
	client, r := newRetryHandler(t, orch)
	taskID := seedFailedRun(t, client, cwd, "implementation", "sess-live", true)

	w := postRetry(t, r, taskID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := orch.resumeID(); got != "sess-live" {
		t.Fatalf("expected ResumeSessionID=sess-live, got %q", got)
	}
	// End-to-end: the resolved id must become a --resume flag in the spawn argv.
	args := pipeline.BuildSpawnArgs(pipeline.SpawnAgentOptions{
		Task: &ent.Task{}, StageRun: &ent.StageRun{}, Prompt: "p", ResumeSessionID: orch.resumeID(),
	})
	assertContains(t, args, "--resume")
	assertContains(t, args, "sess-live")
}

func TestRetry_FreshSpawnWhenNoSessionID(t *testing.T) {
	cwd := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	orch := &captureOrchestrator{}
	client, r := newRetryHandler(t, orch)
	taskID := seedFailedRun(t, client, cwd, "implementation", "", false)

	w := postRetry(t, r, taskID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := orch.resumeID(); got != "" {
		t.Fatalf("expected no ResumeSessionID, got %q", got)
	}
}

func TestRetry_FreshSpawnWhenSessionFileMissing(t *testing.T) {
	cwd := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	orch := &captureOrchestrator{}
	client, r := newRetryHandler(t, orch)
	// session_id recorded, but no JSONL written to disk → must fall back.
	taskID := seedFailedRun(t, client, cwd, "implementation", "sess-gone", false)

	w := postRetry(t, r, taskID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := orch.resumeID(); got != "" {
		t.Fatalf("expected fresh spawn (empty ResumeSessionID), got %q", got)
	}
}

func assertContains(t *testing.T, args []string, want string) {
	t.Helper()
	if !slices.Contains(args, want) {
		t.Fatalf("expected args to contain %q, got %v", want, args)
	}
}
