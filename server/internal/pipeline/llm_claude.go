package pipeline

import (
	"context"
	"fmt"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
)

// ClaudeSpawner is the default LLMSpawner that delegates to SpawnStageAgent.
type ClaudeSpawner struct {
	MCPToken     string
	MCPUrl       string
	AllowGitPush bool
}

func (c *ClaudeSpawner) Name() string { return "claude" }

func (c *ClaudeSpawner) Spawn(_ context.Context, args LLMSpawnArgs) (LLMSpawnResult, error) {
	// Re-use the existing SpawnStageAgent by constructing a minimal Task + StageRun.
	// SpawnStageAgent reads Task.Cwd, Task.WorktreePath, Task.Metadata (for allowGitPush).
	task := &ent.Task{ID: args.TaskID, Cwd: args.WorkDir}
	sr := &ent.StageRun{ID: args.StageRunID}
	opts := SpawnAgentOptions{
		Task:          task,
		StageRun:      sr,
		Prompt:        args.UserPrompt,
		SystemPrompt:  args.SystemPrompt,
		Model:         args.Model,
		EnableChannel: true,
		MCPToken:      c.MCPToken,
		MCPUrl:        c.MCPUrl,
	}
	result, err := SpawnStageAgent(opts)
	if err != nil {
		return LLMSpawnResult{}, fmt.Errorf("ClaudeSpawner.Spawn: %w", err)
	}
	return LLMSpawnResult{PID: result.PID}, nil
}
