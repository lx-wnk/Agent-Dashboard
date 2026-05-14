package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// CustomCommandSpawner runs an arbitrary executable, passes LLMSpawnArgs as JSON
// on stdin, and reads LLMSpawnResult JSON from stdout.
// Set DASHBOARD_SPAWN_COMMAND=/path/to/executable to activate.
type CustomCommandSpawner struct {
	Command string // path to executable
}

func (c *CustomCommandSpawner) Name() string { return "custom" }

func (c *CustomCommandSpawner) Spawn(ctx context.Context, args LLMSpawnArgs) (LLMSpawnResult, error) {
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return LLMSpawnResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Command)
	cmd.Stdin = bytes.NewReader(argsJSON)
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	out, err := cmd.Output()
	if err != nil {
		return LLMSpawnResult{}, fmt.Errorf("CustomCommandSpawner: exec %s: %w: stderr: %s", c.Command, err, stderrBuf.String())
	}
	var result LLMSpawnResult
	if err := json.Unmarshal(out, &result); err != nil {
		return LLMSpawnResult{}, fmt.Errorf("CustomCommandSpawner: decode result: %w", err)
	}
	return result, nil
}
