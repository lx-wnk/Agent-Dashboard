package agentbroadcast

import (
	"context"
	"errors"
	"testing"

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

func TestPipelineTaskEnricher_SetsBothFieldsOnMatch(t *testing.T) {
	stageRuns := fakeStageRuns{bySession: map[string]*ent.StageRun{
		"sess-1": {TaskID: "task-1"},
	}}
	tasks := fakeTasks{byID: map[string]*ent.Task{
		"task-1": {Title: "Implement enricher"},
	}}

	enrich := NewPipelineTaskEnricher(stageRuns, tasks)
	agents := []sdk.Agent{{SessionID: "sess-1"}}
	enrich(context.Background(), agents)

	require.Equal(t, "task-1", agents[0].PipelineTaskID)
	require.Equal(t, "Implement enricher", agents[0].PipelineTaskTitle)
}

func TestPipelineTaskEnricher_NoMatchLeavesEmpty(t *testing.T) {
	stageRuns := fakeStageRuns{bySession: map[string]*ent.StageRun{}}
	tasks := fakeTasks{byID: map[string]*ent.Task{}}

	enrich := NewPipelineTaskEnricher(stageRuns, tasks)
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

	enrich := NewPipelineTaskEnricher(stageRuns, tasks)
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

	enrich := NewPipelineTaskEnricher(stageRuns, tasks)
	agents := []sdk.Agent{{SessionID: "sess-1"}}
	enrich(context.Background(), agents)

	require.Equal(t, "task-1", agents[0].PipelineTaskID)
	require.Empty(t, agents[0].PipelineTaskTitle)
}

func TestPipelineTaskEnricher_NilReposNoop(t *testing.T) {
	enrich := NewPipelineTaskEnricher(nil, nil)
	agents := []sdk.Agent{{SessionID: "sess-1"}}
	require.NotPanics(t, func() { enrich(context.Background(), agents) })
	require.Empty(t, agents[0].PipelineTaskID)
}
