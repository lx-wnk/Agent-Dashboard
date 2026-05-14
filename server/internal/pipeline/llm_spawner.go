package pipeline

import (
	"context"
	"fmt"
)

// LLMSpawnArgs carries everything an LLM adapter needs to run a stage.
type LLMSpawnArgs struct {
	TaskID       string
	StageRunID   string
	// Stage is the pipeline stage name (e.g. "implementation", "self_review").
	// It is set by agentStageHandler.Execute so that PerStageSpawner can route
	// the call to the correct adapter.
	Stage        string
	SystemPrompt string
	UserPrompt   string
	Model        string
	WorkDir      string
	// AllowedTools is reserved for future adapters that can restrict tool use.
	// Current adapters (Ollama, OpenAI, CustomCommand) do not enforce this list.
	AllowedTools []string
	// Env is reserved for future use. Current adapters do not forward these vars.
	Env []string
}

// LLMSpawnResult is returned by a successful Spawn call.
type LLMSpawnResult struct {
	// PID of the spawned process, or 0 for non-subprocess adapters.
	PID int
	// SessionID is a Claude session ID or synthetic ID set by the adapter.
	SessionID string
	// SessionFile is the path to the JSONL file the completion detector reads.
	// For the native Claude path (nil Spawner) this is discovered by the completion detector normally.
	// For non-subprocess adapters this must be written by the adapter itself.
	SessionFile string
}

// LLMSpawner is implemented by each LLM adapter.
type LLMSpawner interface {
	// Name returns the adapter identifier (e.g. "claude", "openai", "ollama").
	Name() string
	// Spawn starts the LLM agent and returns a handle.
	Spawn(ctx context.Context, args LLMSpawnArgs) (LLMSpawnResult, error)
}

// PerStageSpawner routes Spawn calls to per-stage adapters, falling back to the
// default spawner for stages that have no explicit mapping.
// It is built by provideSpawner when AdapterConfig.Stages is non-empty.
type PerStageSpawner struct {
	// DefaultSpawner is used for stages not listed in StageSpawners.
	// May be nil to fall through to the native Claude spawn path for those stages.
	DefaultSpawner LLMSpawner
	// StageSpawners maps pipeline stage names (e.g. "implementation") to their
	// dedicated LLMSpawner. A nil value means use the native Claude spawn path
	// for that specific stage.
	StageSpawners map[string]LLMSpawner
}

// Name returns a composite identifier that makes per-stage dispatch visible in
// audit logs and error messages.
func (p *PerStageSpawner) Name() string { return "per-stage" }

// Spawn dispatches to the stage-specific adapter when one is configured;
// otherwise it falls through to the default adapter.
//
// Returns an error if neither a stage-specific nor a default spawner is available
// for args.Stage. This prevents a nil-pointer panic while producing a clear
// diagnostic message.
func (p *PerStageSpawner) Spawn(ctx context.Context, args LLMSpawnArgs) (LLMSpawnResult, error) {
	if s, ok := p.StageSpawners[args.Stage]; ok {
		return s.Spawn(ctx, args)
	}
	if p.DefaultSpawner == nil {
		return LLMSpawnResult{}, fmt.Errorf("per-stage spawner: no adapter configured for stage %q and default is nil (native Claude path)", args.Stage)
	}
	return p.DefaultSpawner.Spawn(ctx, args)
}
