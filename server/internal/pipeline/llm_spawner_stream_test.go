package pipeline_test

import (
	"context"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
)

// Compile-time guard: ensure the streaming interface is exported and shaped
// correctly. The test body is empty — the assertion is the var declaration.
func TestStreamingLLMSpawner_InterfaceShape(t *testing.T) {
	var _ pipeline.StreamingLLMSpawner = (*fakeStreaming)(nil)
	_ = t // silence unused
}

type fakeStreaming struct{}

func (fakeStreaming) Name() string { return "fake" }
func (fakeStreaming) Spawn(_ context.Context, _ pipeline.LLMSpawnArgs) (pipeline.LLMSpawnResult, error) {
	return pipeline.LLMSpawnResult{}, nil
}
func (fakeStreaming) SpawnStream(_ context.Context, _ pipeline.LLMSpawnArgs) (<-chan string, error) {
	ch := make(chan string)
	close(ch)
	return ch, nil
}
