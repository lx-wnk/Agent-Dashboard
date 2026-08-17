package pipeline_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
)

// dispatchCaptureHandler invokes ctx.DispatchHTTPSpawn with a spawn func that
// reports the context it was handed, so tests can inspect the real seam
// produced by progress_guards.go instead of a hand-built StageContext. spawn
// blocks on release so the seam's own cleanup (which cancels the spawn
// context once spawn returns) cannot race the test's assertions.
type dispatchCaptureHandler struct {
	stage       string
	capturedCtx chan context.Context
	release     chan struct{}
}

func newDispatchCaptureHandler(stage string) *dispatchCaptureHandler {
	return &dispatchCaptureHandler{
		stage:       stage,
		capturedCtx: make(chan context.Context, 1),
		release:     make(chan struct{}),
	}
}

func (h *dispatchCaptureHandler) Stage() string       { return h.stage }
func (h *dispatchCaptureHandler) RequiresAgent() bool { return true }
func (h *dispatchCaptureHandler) Execute(ctx *pipeline.StageContext) (pipeline.StageTransition, error) {
	ctx.DispatchHTTPSpawn(ctx.StageRun.ID, ctx.Task.ID, func(spawnCtx context.Context) (string, error) {
		h.capturedCtx <- spawnCtx
		<-h.release
		return "", nil
	})
	return pipeline.AsyncRunningTransition{PID: 0}, nil
}

// newHTTPSpawnTestTask seeds a task at the "implementation" stage so
// ProgressTask routes through the agent-driven handler override.
func newHTTPSpawnTestTask(t *testing.T, taskRepo repo.TaskRepo, slug string) string {
	t.Helper()
	task, err := taskRepo.Create(context.Background(), repo.CreateTaskInput{
		Slug:                slug,
		Title:               "HTTP Spawn Context Test",
		Cwd:                 "/tmp",
		CurrentStage:        "implementation",
		Priority:            "medium",
		MaxIterations:       3,
		StageTimeoutSeconds: 1800,
	})
	require.NoError(t, err)
	return task.ID
}

func awaitSpawnCtx(t *testing.T, ch chan context.Context) context.Context {
	t.Helper()
	select {
	case spawnCtx := <-ch:
		return spawnCtx
	case <-time.After(2 * time.Second):
		t.Fatal("spawn was never dispatched")
		return nil
	}
}

// TestHTTPSpawnSeam_RequestCancellationDoesNotReachSpawn proves that cancelling
// the caller's (HTTP request) context after ProgressTask returns — mirroring the
// handler returning to net/http — does not cancel the context handed to spawn.
func TestHTTPSpawnSeam_RequestCancellationDoesNotReachSpawn(t *testing.T) {
	orch, taskRepo := makeOrchFromBundle(t, openSharedBundle(t))

	handler := newDispatchCaptureHandler("implementation")
	orch.SetHandlerOverride("implementation", handler)

	taskID := newHTTPSpawnTestTask(t, taskRepo, "req-cancel-no-reach-spawn")

	reqCtx, cancel := context.WithCancel(context.Background())
	_, err := orch.ProgressTask(reqCtx, taskID, nil)
	require.NoError(t, err)
	cancel() // simulate the HTTP handler returning and net/http cancelling r.Context()

	spawnCtx := awaitSpawnCtx(t, handler.capturedCtx)
	require.NoError(t, spawnCtx.Err(), "spawn ctx must not be cancelled by the finished HTTP request")
	close(handler.release)
}

// TestHTTPSpawnSeam_ShutdownCancelsSpawn proves that cancelling the
// orchestrator's base (long-lived) context still cancels an in-flight spawn.
func TestHTTPSpawnSeam_ShutdownCancelsSpawn(t *testing.T) {
	orch, taskRepo := makeOrchFromBundle(t, openSharedBundle(t))

	handler := newDispatchCaptureHandler("implementation")
	orch.SetHandlerOverride("implementation", handler)

	baseCtx, baseCancel := context.WithCancel(context.Background())
	t.Cleanup(baseCancel)
	orch.SetBaseContextForTest(baseCtx)

	taskID := newHTTPSpawnTestTask(t, taskRepo, "shutdown-cancels-spawn")

	_, err := orch.ProgressTask(context.Background(), taskID, nil)
	require.NoError(t, err)

	spawnCtx := awaitSpawnCtx(t, handler.capturedCtx)
	require.NoError(t, spawnCtx.Err(), "spawn ctx must be alive before shutdown")

	baseCancel()

	select {
	case <-spawnCtx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("spawn ctx was not cancelled after orchestrator shutdown")
	}
	close(handler.release)
}

// TestHTTPSpawnSeam_ValuesSurvive proves that values attached to the caller's
// context (e.g. a trace ID) remain readable from the spawn context.
func TestHTTPSpawnSeam_ValuesSurvive(t *testing.T) {
	orch, taskRepo := makeOrchFromBundle(t, openSharedBundle(t))

	handler := newDispatchCaptureHandler("implementation")
	orch.SetHandlerOverride("implementation", handler)

	taskID := newHTTPSpawnTestTask(t, taskRepo, "values-survive")

	type ctxKey string
	const traceIDKey ctxKey = "traceID"
	reqCtx := context.WithValue(context.Background(), traceIDKey, "trace-abc-123")

	_, err := orch.ProgressTask(reqCtx, taskID, nil)
	require.NoError(t, err)

	spawnCtx := awaitSpawnCtx(t, handler.capturedCtx)
	require.Equal(t, "trace-abc-123", spawnCtx.Value(traceIDKey))
	close(handler.release)
}
