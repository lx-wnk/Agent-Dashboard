package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// OllamaSpawner calls the Ollama HTTP API synchronously and writes a
// synthetic JSONL session file so the completion detector can parse the output.
type OllamaSpawner struct {
	Host         string // e.g. "http://localhost:11434"
	DefaultModel string
	// client is shared across calls to reuse the underlying connection pool.
	// Initialized lazily on the first Spawn call when nil (e.g. struct-literal construction).
	client *http.Client
}

func (o *OllamaSpawner) Name() string { return "ollama" }

func (o *OllamaSpawner) Spawn(ctx context.Context, args LLMSpawnArgs) (LLMSpawnResult, error) {
	model := args.Model
	if model == "" {
		model = o.DefaultModel
	}
	host := o.Host
	if host == "" {
		host = "http://localhost:11434"
	}

	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type request struct {
		Model    string    `json:"model"`
		Messages []message `json:"messages"`
		Stream   bool      `json:"stream"`
	}
	body, err := json.Marshal(request{
		Model: model,
		Messages: []message{
			{Role: "system", Content: args.SystemPrompt},
			{Role: "user", Content: args.UserPrompt},
		},
		Stream: false,
	})
	if err != nil {
		return LLMSpawnResult{}, fmt.Errorf("OllamaSpawner: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, host+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return LLMSpawnResult{}, fmt.Errorf("OllamaSpawner: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if o.client == nil {
		o.client = &http.Client{Timeout: 5 * time.Minute}
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return LLMSpawnResult{}, fmt.Errorf("OllamaSpawner: POST /api/chat: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return LLMSpawnResult{}, fmt.Errorf("OllamaSpawner: HTTP %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return LLMSpawnResult{}, fmt.Errorf("OllamaSpawner: decode response: %w", err)
	}

	sessionFile, err := writeSyntheticSession(args.WorkDir, args.StageRunID, result.Message.Content)
	if err != nil {
		return LLMSpawnResult{}, err
	}
	return LLMSpawnResult{PID: 0, SessionID: "ollama-" + args.StageRunID, SessionFile: sessionFile}, nil
}

// writeSyntheticSession writes a single-line JSONL file in the format the
// completion detector expects: one assistant message whose text contains the
// ```json ... ``` block produced by the LLM.
//
// The parser (session_reader.go / parseJsonlLines) expects each line to match
// JsonlEntry: {"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"..."}]}}
func writeSyntheticSession(workDir, stageRunID, content string) (string, error) {
	dir := filepath.Join(os.TempDir(), "dashboard-synthetic-sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("writeSyntheticSession: mkdir: %w", err)
	}
	sessionID := "synthetic-" + stageRunID
	path := filepath.Join(dir, sessionID+".jsonl")

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
	entry := line{
		Type: "assistant",
		Message: message{
			Role: "assistant",
			Content: []contentBlock{
				{Type: "text", Text: content},
			},
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return "", fmt.Errorf("writeSyntheticSession: write: %w", err)
	}
	return path, nil
}
