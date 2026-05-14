package repo_test

import (
	"context"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/stretchr/testify/require"
)

func TestAuditRepo_AppendAndList(t *testing.T) {
	client := openDB(t)
	ctx := context.Background()
	tr := repo.NewTaskRepo(client)
	ar := repo.NewAuditRepo(client)

	taskID := createTask(t, tr, "audit-task-1")

	err := ar.Append(ctx, repo.AppendAuditInput{
		TaskID: taskID, Actor: "user:alice", Action: "create_task",
	})
	require.NoError(t, err)

	err = ar.Append(ctx, repo.AppendAuditInput{
		TaskID:  taskID,
		Actor:   "system",
		Action:  "stage_transition",
		Details: map[string]any{"from": "concept", "to": "implementation"},
	})
	require.NoError(t, err)

	logs, err := ar.ListForTask(ctx, taskID)
	require.NoError(t, err)
	require.Len(t, logs, 2)
	require.Equal(t, "create_task", logs[0].Action)
	require.Equal(t, "stage_transition", logs[1].Action)
}
