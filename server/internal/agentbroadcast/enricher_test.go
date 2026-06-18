package agentbroadcast

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// fakeStageRuns embeds repo.StageRunRepo so only GetBySessionID needs an
// implementation; any other method call would panic (none are reached here).
type fakeStageRuns struct {
	repo.StageRunRepo
	bySession map[string]*ent.StageRun
	err       error
}

func (f fakeStageRuns) GetBySessionID(_ context.Context, sessionID string) (*ent.StageRun, error) {
	if f.err != nil {
		return nil, f.err
	}
	sr, ok := f.bySession[sessionID]
	if !ok {
		return nil, errors.New("stagerun.GetBySessionID: not found")
	}
	return sr, nil
}

type fakeTasks struct {
	repo.TaskRepo
	byID map[string]*ent.Task
	err  error
}

func (f fakeTasks) GetByID(_ context.Context, id string) (*ent.Task, error) {
	if f.err != nil {
		return nil, f.err
	}
	t, ok := f.byID[id]
	if !ok {
		return nil, errors.New("task.GetByID: not found")
	}
	return t, nil
}

// fakePermissions embeds repo.PermissionRepo so only ListPendingForStageRun
// needs an implementation.
type fakePermissions struct {
	repo.PermissionRepo
	byStageRun map[string][]*ent.PermissionRequest
	err        error
}

func (f fakePermissions) ListPendingForStageRun(_ context.Context, stageRunID string) ([]*ent.PermissionRequest, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byStageRun[stageRunID], nil
}

func TestPipelineTaskEnricher_SetsBothFieldsOnMatch(t *testing.T) {
	stageRuns := fakeStageRuns{bySession: map[string]*ent.StageRun{
		"sess-1": {TaskID: "task-1"},
	}}
	tasks := fakeTasks{byID: map[string]*ent.Task{
		"task-1": {Title: "Implement enricher"},
	}}

	enrich := NewPipelineTaskEnricher(stageRuns, tasks, nil)
	agents := []sdk.Agent{{SessionID: "sess-1"}}
	enrich(context.Background(), agents)

	require.Equal(t, "task-1", agents[0].PipelineTaskID)
	require.Equal(t, "Implement enricher", agents[0].PipelineTaskTitle)
}

func TestPipelineTaskEnricher_NoMatchLeavesEmpty(t *testing.T) {
	stageRuns := fakeStageRuns{bySession: map[string]*ent.StageRun{}}
	tasks := fakeTasks{byID: map[string]*ent.Task{}}

	enrich := NewPipelineTaskEnricher(stageRuns, tasks, nil)
	agents := []sdk.Agent{{SessionID: "sess-unknown"}, {SessionID: ""}}
	enrich(context.Background(), agents)

	require.Empty(t, agents[0].PipelineTaskID)
	require.Empty(t, agents[0].PipelineTaskTitle)
	require.Empty(t, agents[1].PipelineTaskID)
	require.Empty(t, agents[1].PipelineTaskTitle)
}

func TestPipelineTaskEnricher_StageRunErrorLeavesEmptyNoPanic(t *testing.T) {
	stageRuns := fakeStageRuns{err: errors.New("db down")}
	tasks := fakeTasks{}

	enrich := NewPipelineTaskEnricher(stageRuns, tasks, nil)
	agents := []sdk.Agent{{SessionID: "sess-1"}}
	require.NotPanics(t, func() { enrich(context.Background(), agents) })

	require.Empty(t, agents[0].PipelineTaskID)
	require.Empty(t, agents[0].PipelineTaskTitle)
}

func TestPipelineTaskEnricher_TaskErrorKeepsIDDropsTitle(t *testing.T) {
	stageRuns := fakeStageRuns{bySession: map[string]*ent.StageRun{
		"sess-1": {TaskID: "task-1"},
	}}
	tasks := fakeTasks{err: errors.New("db down")}

	enrich := NewPipelineTaskEnricher(stageRuns, tasks, nil)
	agents := []sdk.Agent{{SessionID: "sess-1"}}
	enrich(context.Background(), agents)

	require.Equal(t, "task-1", agents[0].PipelineTaskID)
	require.Empty(t, agents[0].PipelineTaskTitle)
}

func TestPipelineTaskEnricher_NilReposNoop(t *testing.T) {
	enrich := NewPipelineTaskEnricher(nil, nil, nil)
	agents := []sdk.Agent{{SessionID: "sess-1"}}
	require.NotPanics(t, func() { enrich(context.Background(), agents) })
	require.Empty(t, agents[0].PipelineTaskID)
}

func TestPipelineTaskEnricher_PendingPermissions_TwoRequests(t *testing.T) {
	pattern := "*.go"
	reason := "need read"
	ts := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	stageRuns := fakeStageRuns{bySession: map[string]*ent.StageRun{
		"sess-1": {ID: "sr-1", TaskID: "task-1"},
	}}
	tasks := fakeTasks{byID: map[string]*ent.Task{
		"task-1": {Title: "Perm task"},
	}}
	perms := fakePermissions{byStageRun: map[string][]*ent.PermissionRequest{
		"sr-1": {
			{ID: "req-1", Tool: "Bash", RequestedAt: ts},
			{ID: "req-2", Tool: "Read", Pattern: &pattern, Reason: &reason, RequestedAt: ts},
		},
	}}

	enrich := NewPipelineTaskEnricher(stageRuns, tasks, perms)
	agents := []sdk.Agent{{SessionID: "sess-1"}}
	enrich(context.Background(), agents)

	require.Len(t, agents[0].PendingPermissions, 2)
	require.Equal(t, "req-1", agents[0].PendingPermissions[0].ID)
	require.Equal(t, "Bash", agents[0].PendingPermissions[0].Tool)
	require.Equal(t, "req-2", agents[0].PendingPermissions[1].ID)
	require.Equal(t, "Read", agents[0].PendingPermissions[1].Tool)
	require.Equal(t, &pattern, agents[0].PendingPermissions[1].Pattern)
	require.Equal(t, "2025-01-15T10:00:00Z", agents[0].PendingPermissions[0].RequestedAt)
}

func TestPipelineTaskEnricher_PendingPermissions_NoStageRun(t *testing.T) {
	stageRuns := fakeStageRuns{bySession: map[string]*ent.StageRun{}}
	tasks := fakeTasks{byID: map[string]*ent.Task{}}
	perms := fakePermissions{byStageRun: map[string][]*ent.PermissionRequest{}}

	enrich := NewPipelineTaskEnricher(stageRuns, tasks, perms)
	agents := []sdk.Agent{{SessionID: "sess-no-run"}}
	enrich(context.Background(), agents)

	require.Empty(t, agents[0].PendingPermissions)
}

func TestPipelineTaskEnricher_PendingPermissions_ResolvedNotIncluded(t *testing.T) {
	// ListPendingForStageRun only returns pending (outcome IS NULL) — resolved
	// ones are excluded by the repo. Verify the enricher passes through exactly
	// what the repo returns: no resolved request in the result.
	ts := time.Now().UTC()
	stageRuns := fakeStageRuns{bySession: map[string]*ent.StageRun{
		"sess-1": {ID: "sr-1", TaskID: "task-1"},
	}}
	tasks := fakeTasks{byID: map[string]*ent.Task{
		"task-1": {Title: "T"},
	}}
	// Fake repo returns only the one still-pending request (resolved ones are
	// filtered at the repo layer, not here).
	perms := fakePermissions{byStageRun: map[string][]*ent.PermissionRequest{
		"sr-1": {{ID: "req-pending", Tool: "Write", RequestedAt: ts}},
	}}

	enrich := NewPipelineTaskEnricher(stageRuns, tasks, perms)
	agents := []sdk.Agent{{SessionID: "sess-1"}}
	enrich(context.Background(), agents)

	require.Len(t, agents[0].PendingPermissions, 1)
	require.Equal(t, "req-pending", agents[0].PendingPermissions[0].ID)
}

func TestPipelineTaskEnricher_PendingPermissions_QueryErrorLeavesEmpty(t *testing.T) {
	stageRuns := fakeStageRuns{bySession: map[string]*ent.StageRun{
		"sess-1": {ID: "sr-1", TaskID: "task-1"},
	}}
	tasks := fakeTasks{byID: map[string]*ent.Task{
		"task-1": {Title: "T"},
	}}
	perms := fakePermissions{err: errors.New("db down")}

	enrich := NewPipelineTaskEnricher(stageRuns, tasks, perms)
	agents := []sdk.Agent{{SessionID: "sess-1"}}
	require.NotPanics(t, func() { enrich(context.Background(), agents) })

	// Task fields still populated; permissions silently empty.
	require.Equal(t, "task-1", agents[0].PipelineTaskID)
	require.Empty(t, agents[0].PendingPermissions)
}
