package pipeline

import "context"

// LLMSpawnArgs carries everything an LLM adapter needs to run a stage.
type LLMSpawnArgs struct {
	TaskID       string
	StageRunID   string
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
	// SessionID is a Claude session ID (for ClaudeSpawner) or synthetic ID.
	SessionID string
	// SessionFile is the path to the JSONL file the completion detector reads.
	// For ClaudeSpawner this is discovered by the completion detector normally.
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
