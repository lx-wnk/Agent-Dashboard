package serverapp

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/kontor/server/internal/db"
	"github.com/lx-wnk/kontor/server/internal/db/rawrepo"
	"github.com/lx-wnk/kontor/server/internal/db/repo"
	"github.com/lx-wnk/kontor/server/internal/sse"
)

func TestNewMCPTaskBroadcast_TaskCreated_SendsEnrichedPayload(t *testing.T) {
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })

	taskRepo := repo.NewTaskRepo(bundle.Client)
	srRepo := repo.NewStageRunRepo(bundle.Client)
	permRepo := repo.NewPermissionRepo(bundle.Client)
	srBulkRepo := rawrepo.NewStageRunBulkRepo(bundle.DB)

	ctx := context.Background()
	task, err := taskRepo.Create(ctx, repo.CreateTaskInput{
		Slug:          "di-mcp-broadcast",
		Title:         "DI MCP Broadcast",
		Cwd:           t.TempDir(),
		MaxIterations: 3,
		Priority:      "normal",
		CurrentStage:  "backlog",
	})
	require.NoError(t, err)

	tb := sse.NewTaskBroadcaster(sse.NewBroadcaster())
	ch := tb.Subscribe()
	t.Cleanup(func() { tb.Unsubscribe(ch) })
	broadcast := newMCPTaskBroadcast(taskRepo, srRepo, permRepo, srBulkRepo, tb)

	broadcast(ctx, "task_created", task.ID)

	var frame []byte
	select {
	case frame = <-ch:
	default:
		t.Fatal("broadcast must have published a frame")
	}
	frame = bytes.TrimSuffix(bytes.TrimPrefix(frame, []byte("data: ")), []byte("\n\n"))

	var event sse.TaskEvent
	require.NoError(t, json.Unmarshal(frame, &event))
	require.Equal(t, "task_created", event.Type)
	require.Equal(t, task.ID, event.TaskID)

	payload, ok := event.Payload.(map[string]any)
	require.True(t, ok, "payload must be a JSON object")
	require.NotEmpty(t, payload, "payload must not be empty")
	require.Equal(t, task.ID, payload["id"])
	require.Equal(t, "di-mcp-broadcast", payload["slug"])
}
