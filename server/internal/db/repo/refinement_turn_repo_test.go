package repo_test

import (
	"context"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/stretchr/testify/require"
)

// TestRefinementTurnRepo_ListForTaskNewest_Order verifies that ListForTaskNewest
// returns turns oldest-first (ASC) after fetching them newest-first from the DB
// and reversing the slice internally.
func TestRefinementTurnRepo_ListForTaskNewest_Order(t *testing.T) {
	client := openTestDB(t)
	taskRepo := repo.NewTaskRepo(client)
	turnRepo := repo.NewRefinementTurnRepo(client)
	ctx := context.Background()

	taskID := createTask(t, taskRepo, "refine-order-test")

	// Insert turn A first.
	turnA, err := turnRepo.Create(ctx, repo.CreateTurnInput{
		TaskID:  taskID,
		Role:    "user",
		Content: "first message",
	})
	require.NoError(t, err)

	// Insert turn B second.
	turnB, err := turnRepo.Create(ctx, repo.CreateTurnInput{
		TaskID:  taskID,
		Role:    "assistant",
		Content: "second message",
	})
	require.NoError(t, err)

	// ListForTaskNewest with limit 2 should return both, oldest first (A then B).
	turns, err := turnRepo.ListForTaskNewest(ctx, taskID, 2)
	require.NoError(t, err)
	require.Len(t, turns, 2)

	require.Equal(t, turnA.ID, turns[0].ID,
		"first element must be the oldest turn (A)")
	require.Equal(t, turnB.ID, turns[1].ID,
		"second element must be the newest turn (B)")
}

// TestRefinementTurnRepo_ListForTaskNewest_Limit verifies that the limit parameter
// returns only the N most-recent turns (still in ASC order).
func TestRefinementTurnRepo_ListForTaskNewest_Limit(t *testing.T) {
	client := openTestDB(t)
	taskRepo := repo.NewTaskRepo(client)
	turnRepo := repo.NewRefinementTurnRepo(client)
	ctx := context.Background()

	taskID := createTask(t, taskRepo, "refine-limit-test")

	for i := 0; i < 5; i++ {
		_, err := turnRepo.Create(ctx, repo.CreateTurnInput{
			TaskID:  taskID,
			Role:    "user",
			Content: "message",
		})
		require.NoError(t, err)
	}

	// With limit 3 we should only get the 3 most recent turns.
	turns, err := turnRepo.ListForTaskNewest(ctx, taskID, 3)
	require.NoError(t, err)
	require.Len(t, turns, 3)
}
