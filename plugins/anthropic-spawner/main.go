package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
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

// spawnTimeout bounds a single API call so a stalled stream or request cannot
// hang the refine turn — SpawnStream applies no timeout of its own.
const spawnTimeout = 5 * time.Minute

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

func newClient() anthropic.Client {
	var opts []option.RequestOption
	if base := os.Getenv("ANTHROPIC_BASE_URL"); base != "" {
		opts = append(opts, option.WithBaseURL(base))
	}
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		opts = append(opts, option.WithAPIKey(key))
	}
	return anthropic.NewClient(opts...)
}

func messageParams(args spawnArgs, maxTokens int64) anthropic.MessageNewParams {
	p := anthropic.MessageNewParams{
		Model:     anthropic.Model(args.Model),
		MaxTokens: maxTokens,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(args.UserPrompt)),
		},
	}
	if args.SystemPrompt != "" {
		p.System = []anthropic.TextBlockParam{{Text: args.SystemPrompt}}
	}
	return p
}

func runOnce(args spawnArgs, stdout io.Writer) error {
	client := newClient()
	ctx, cancel := context.WithTimeout(context.Background(), spawnTimeout)
	defer cancel()
	msg, err := client.Messages.New(ctx, messageParams(args, 16000))
	if err != nil {
		return fmt.Errorf("messages.new: %w", err)
	}
	var sb strings.Builder
	for _, block := range msg.Content {
		if block.Type == "text" {
			sb.WriteString(block.Text)
		}
	}
	if msg.StopReason == anthropic.StopReasonRefusal {
		return fmt.Errorf("request refused by safety classifier: %s", strings.TrimSpace(sb.String()))
	}
	if msg.StopReason == anthropic.StopReasonMaxTokens {
		return fmt.Errorf("response truncated at max_tokens; increase MaxTokens")
	}
	sessionFile, err := writeSyntheticSession(args.StageRunID, sb.String())
	if err != nil {
		return err
	}
	res := spawnResult{PID: 0, SessionID: "anthropic-" + args.StageRunID, SessionFile: sessionFile}
	enc, err := json.Marshal(res)
	if err != nil {
		return err
	}
	_, err = stdout.Write(enc)
	return err
}

func writeSyntheticSession(stageRunID, content string) (string, error) {
	dir := filepath.Join(os.TempDir(), "dashboard-synthetic-sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("writeSyntheticSession: mkdir: %w", err)
	}
	path := filepath.Join(dir, "anthropic-"+stageRunID+".jsonl")
	type contentBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type message struct {
		Role      string         `json:"role"`
		Content   []contentBlock `json:"content"`
		Timestamp string         `json:"timestamp"`
	}
	type line struct {
		Type    string  `json:"type"`
		Message message `json:"message"`
	}
	entry := line{Type: "assistant", Message: message{
		Role:      "assistant",
		Content:   []contentBlock{{Type: "text", Text: content}},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}}
	data, err := json.Marshal(entry)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return "", fmt.Errorf("writeSyntheticSession: write: %w", err)
	}
	return path, nil
}

func runStream(args spawnArgs, stdout io.Writer) error {
	client := newClient()
	ctx, cancel := context.WithTimeout(context.Background(), spawnTimeout)
	defer cancel()
	stream := client.Messages.NewStreaming(ctx, messageParams(args, 64000))
	acc := anthropic.Message{}
	for stream.Next() {
		event := stream.Current()
		if err := acc.Accumulate(event); err != nil {
			fmt.Fprintln(os.Stderr, "anthropic-spawner: accumulate:", err)
		}
		switch ev := event.AsAny().(type) {
		case anthropic.ContentBlockDeltaEvent:
			if d, ok := ev.Delta.AsAny().(anthropic.TextDelta); ok && d.Text != "" {
				if _, err := fmt.Fprintln(stdout, d.Text); err != nil {
					return err
				}
			}
		}
	}
	if err := stream.Err(); err != nil {
		return fmt.Errorf("messages stream: %w", err)
	}
	if acc.StopReason == anthropic.StopReasonRefusal {
		var sb strings.Builder
		for _, block := range acc.Content {
			if block.Type == "text" {
				sb.WriteString(block.Text)
			}
		}
		return fmt.Errorf("request refused by safety classifier: %s", strings.TrimSpace(sb.String()))
	}
	if acc.StopReason == anthropic.StopReasonMaxTokens {
		return fmt.Errorf("response truncated at max_tokens; increase MaxTokens")
	}
	return nil
}
