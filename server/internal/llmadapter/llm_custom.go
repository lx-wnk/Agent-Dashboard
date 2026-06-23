package llmadapter

import (
	"bufio"
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
	args.Stream = false
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

// SpawnStream exec's the custom command with LLMSpawnArgs JSON on stdin and
// scans stdout line by line. Each non-empty line is emitted as a chunk. The
// channel closes when the process exits or the context is cancelled.
func (c *CustomCommandSpawner) SpawnStream(ctx context.Context, args LLMSpawnArgs) (<-chan string, error) {
	args.Stream = true
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("CustomCommandSpawner.SpawnStream: marshal args: %w", err)
	}
	cmd := exec.CommandContext(ctx, c.Command)
	cmd.Stdin = bytes.NewReader(argsJSON)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("CustomCommandSpawner.SpawnStream: stdout pipe: %w", err)
	}
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("CustomCommandSpawner.SpawnStream: start %s: %w", c.Command, err)
	}

	ch := make(chan string, 16)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			select {
			case ch <- line:
			case <-ctx.Done():
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return
			}
		}
		if err := cmd.Wait(); err != nil {
			msg := "[ERROR] CustomCommandSpawner.SpawnStream: " + err.Error()
			if s := stderrBuf.String(); s != "" {
				msg += " — " + s
			}
			ch <- msg
		}
	}()
	return ch, nil
}
