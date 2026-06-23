package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// spawnArgs mirrors server llmadapter.LLMSpawnArgs (field names must match so
// the JSON the server marshals round-trips here). Only the fields this binary
// uses are listed; extra JSON keys are ignored by encoding/json.
type spawnArgs struct {
	TaskID       string
	StageRunID   string
	Stage        string
	SystemPrompt string
	UserPrompt   string
	Model        string
	WorkDir      string
	Stream       bool
}

// spawnResult mirrors server llmadapter.LLMSpawnResult (no json tags there, so
// marshaling produces PascalCase keys; the server's json.Unmarshal is
// case-insensitive but we match exactly to be safe).
type spawnResult struct {
	PID         int
	SessionID   string
	SessionFile string
}

const defaultModel = "claude-opus-4-8"

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "anthropic-spawner:", err)
		os.Exit(1)
	}
}

func run(stdin io.Reader, stdout io.Writer) error {
	raw, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	var args spawnArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return fmt.Errorf("decode LLMSpawnArgs: %w", err)
	}
	if args.Model == "" {
		args.Model = defaultModel
	}
	if args.Stream {
		return runStream(args, stdout)
	}
	return runOnce(args, stdout)
}

// runOnce / runStream are implemented in Task 4 / Task 5.
func runOnce(args spawnArgs, stdout io.Writer) error   { return fmt.Errorf("runOnce not implemented") }
func runStream(args spawnArgs, stdout io.Writer) error { return fmt.Errorf("runStream not implemented") }
