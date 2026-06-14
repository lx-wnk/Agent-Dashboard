package llmadapter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// OpenAISpawner calls any OpenAI-compatible chat completions endpoint.
type OpenAISpawner struct {
	BaseURL      string // e.g. "https://api.openai.com/v1"
	APIKeyEnv    string // env var name holding the API key, e.g. "OPENAI_API_KEY"
	DefaultModel string
	clientOnce   sync.Once
	client       *http.Client
}

func (o *OpenAISpawner) Name() string { return "openai" }

func (o *OpenAISpawner) Spawn(ctx context.Context, args LLMSpawnArgs) (LLMSpawnResult, error) {
	model := args.Model
	if model == "" {
		model = o.DefaultModel
	}
	baseURL := o.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	apiKey := os.Getenv(o.APIKeyEnv)

	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type request struct {
		Model    string    `json:"model"`
		Messages []message `json:"messages"`
	}
	body, err := json.Marshal(request{
		Model: model,
		Messages: []message{
			{Role: "system", Content: args.SystemPrompt},
			{Role: "user", Content: args.UserPrompt},
		},
	})
	if err != nil {
		return LLMSpawnResult{}, fmt.Errorf("OpenAISpawner: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return LLMSpawnResult{}, fmt.Errorf("OpenAISpawner: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	o.clientOnce.Do(func() { o.client = &http.Client{Timeout: 5 * time.Minute} })
	resp, err := o.client.Do(req)
	if err != nil {
		return LLMSpawnResult{}, fmt.Errorf("OpenAISpawner: POST completions: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return LLMSpawnResult{}, fmt.Errorf("OpenAISpawner: HTTP %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return LLMSpawnResult{}, fmt.Errorf("OpenAISpawner: decode: %w", err)
	}
	if len(result.Choices) == 0 {
		return LLMSpawnResult{}, fmt.Errorf("OpenAISpawner: no choices in response")
	}

	content := result.Choices[0].Message.Content
	sessionFile, err := writeSyntheticSession(args.WorkDir, args.StageRunID, content)
	if err != nil {
		return LLMSpawnResult{}, err
	}
	return LLMSpawnResult{PID: 0, SessionID: "openai-" + args.StageRunID, SessionFile: sessionFile}, nil
}

// SpawnStream calls OpenAI's /v1/chat/completions with stream:true and emits
// delta.content tokens on the returned channel. The channel is closed when the
// stream ends, [DONE] is received, or the context is cancelled.
func (o *OpenAISpawner) SpawnStream(ctx context.Context, args LLMSpawnArgs) (<-chan string, error) {
	model := args.Model
	if model == "" {
		model = o.DefaultModel
	}
	baseURL := o.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	apiKey := os.Getenv(o.APIKeyEnv)
	if apiKey == "" {
		return nil, fmt.Errorf("OpenAISpawner.SpawnStream: %s not set", o.APIKeyEnv)
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
		Stream: true,
	})
	if err != nil {
		return nil, fmt.Errorf("OpenAISpawner.SpawnStream: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("OpenAISpawner.SpawnStream: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	o.clientOnce.Do(func() { o.client = &http.Client{Timeout: 10 * time.Minute} })
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OpenAISpawner.SpawnStream: POST: %w", err)
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("OpenAISpawner.SpawnStream: HTTP %d: %s", resp.StatusCode, body)
	}

	ch := make(chan string, 16)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			payload := strings.TrimPrefix(line, "data: ")
			if payload == "[DONE]" {
				return
			}
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
				ch <- "[ERROR] OpenAISpawner.SpawnStream: decode: " + err.Error()
				return
			}
			for _, c := range chunk.Choices {
				if c.Delta.Content == "" {
					continue
				}
				select {
				case ch <- c.Delta.Content:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return ch, nil
}
