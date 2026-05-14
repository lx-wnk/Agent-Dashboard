package pipeline_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
)

func TestOllamaSpawner_Spawn_WritesSessionFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/chat", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]any{"content": "```json\n{\"summary\":\"ok\"}\n```"},
		})
	}))
	defer srv.Close()

	spawner := &pipeline.OllamaSpawner{Host: srv.URL, DefaultModel: "llama3"}
	result, err := spawner.Spawn(context.Background(), pipeline.LLMSpawnArgs{
		TaskID:       "t1",
		StageRunID:   "sr1",
		UserPrompt:   "do the thing",
		SystemPrompt: "you are helpful",
		WorkDir:      t.TempDir(),
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.PID)
	assert.NotEmpty(t, result.SessionFile)

	// Session file must exist and contain valid JSONL.
	data, err := os.ReadFile(result.SessionFile)
	require.NoError(t, err)
	assert.Contains(t, string(data), "assistant")

	// Cleanup
	os.Remove(result.SessionFile)
}

func TestOpenAISpawner_Spawn_WritesSessionFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/chat/completions", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]any{"content": "```json\n{\"result\":\"done\"}\n```"}},
			},
		})
	}))
	defer srv.Close()

	spawner := &pipeline.OpenAISpawner{BaseURL: srv.URL, DefaultModel: "gpt-4o"}
	result, err := spawner.Spawn(context.Background(), pipeline.LLMSpawnArgs{
		TaskID:     "t2",
		StageRunID: "sr2",
		UserPrompt: "do a thing",
		WorkDir:    t.TempDir(),
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.PID)
	assert.NotEmpty(t, result.SessionFile)

	data, err := os.ReadFile(result.SessionFile)
	require.NoError(t, err)
	assert.Contains(t, string(data), "assistant")

	os.Remove(result.SessionFile)
}

func TestClaudeSpawner_Name(t *testing.T) {
	s := &pipeline.ClaudeSpawner{}
	assert.Equal(t, "claude", s.Name())
}

func TestOllamaSpawner_Name(t *testing.T) {
	s := &pipeline.OllamaSpawner{}
	assert.Equal(t, "ollama", s.Name())
}

func TestOpenAISpawner_Name(t *testing.T) {
	s := &pipeline.OpenAISpawner{}
	assert.Equal(t, "openai", s.Name())
}

func TestCustomCommandSpawner_Name(t *testing.T) {
	s := &pipeline.CustomCommandSpawner{Command: "/bin/echo"}
	assert.Equal(t, "custom", s.Name())
}
