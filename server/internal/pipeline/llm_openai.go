package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

// OpenAISpawner calls any OpenAI-compatible chat completions endpoint.
type OpenAISpawner struct {
	BaseURL      string // e.g. "https://api.openai.com/v1"
	APIKeyEnv    string // env var name holding the API key, e.g. "OPENAI_API_KEY"
	DefaultModel string
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
	body, _ := json.Marshal(request{
		Model: model,
		Messages: []message{
			{Role: "system", Content: args.SystemPrompt},
			{Role: "user", Content: args.UserPrompt},
		},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return LLMSpawnResult{}, fmt.Errorf("OpenAISpawner: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return LLMSpawnResult{}, fmt.Errorf("OpenAISpawner: POST completions: %w", err)
	}
	defer resp.Body.Close()

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
