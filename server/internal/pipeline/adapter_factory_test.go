package pipeline_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
)

func TestNewLLMSpawnerFromSpawner_Nil(t *testing.T) {
	got, err := pipeline.NewLLMSpawnerFromSpawner(nil)
	require.NoError(t, err)
	assert.Nil(t, got, "nil spawner row must yield nil adapter (caller falls back to native path)")
}

func TestNewLLMSpawnerFromSpawner_ClaudeAndEmpty(t *testing.T) {
	for _, adapterType := range []string{"", "claude"} {
		t.Run("adapter_type="+adapterType, func(t *testing.T) {
			row := &ent.Spawner{AdapterType: adapterType}
			got, err := pipeline.NewLLMSpawnerFromSpawner(row)
			require.NoError(t, err)
			assert.Nil(t, got, "claude/empty adapter_type must yield nil adapter")
		})
	}
}

func TestNewLLMSpawnerFromSpawner_Ollama(t *testing.T) {
	row := &ent.Spawner{
		AdapterType: "ollama",
		AdapterConfig: map[string]string{
			"host":          "http://10.0.0.5:11434",
			"default_model": "llama3:8b",
		},
	}
	got, err := pipeline.NewLLMSpawnerFromSpawner(row)
	require.NoError(t, err)
	require.NotNil(t, got)
	o, ok := got.(*pipeline.OllamaSpawner)
	require.True(t, ok, "expected *OllamaSpawner, got %T", got)
	assert.Equal(t, "http://10.0.0.5:11434", o.Host)
	assert.Equal(t, "llama3:8b", o.DefaultModel)
	assert.Equal(t, "ollama", got.Name())
}

func TestNewLLMSpawnerFromSpawner_OpenAI(t *testing.T) {
	row := &ent.Spawner{
		AdapterType: "openai",
		AdapterConfig: map[string]string{
			"base_url":      "https://api.openai.com/v1",
			"api_key_env":   "OPENAI_API_KEY",
			"default_model": "gpt-4o-mini",
		},
	}
	got, err := pipeline.NewLLMSpawnerFromSpawner(row)
	require.NoError(t, err)
	require.NotNil(t, got)
	o, ok := got.(*pipeline.OpenAISpawner)
	require.True(t, ok, "expected *OpenAISpawner, got %T", got)
	assert.Equal(t, "https://api.openai.com/v1", o.BaseURL)
	assert.Equal(t, "OPENAI_API_KEY", o.APIKeyEnv)
	assert.Equal(t, "gpt-4o-mini", o.DefaultModel)
}

func TestNewLLMSpawnerFromSpawner_CustomWithCommand(t *testing.T) {
	row := &ent.Spawner{
		AdapterType: "custom",
		Command:     "/usr/local/bin/my-llm-shim",
	}
	got, err := pipeline.NewLLMSpawnerFromSpawner(row)
	require.NoError(t, err)
	require.NotNil(t, got)
	c, ok := got.(*pipeline.CustomCommandSpawner)
	require.True(t, ok, "expected *CustomCommandSpawner, got %T", got)
	assert.Equal(t, "/usr/local/bin/my-llm-shim", c.Command)
}

func TestNewLLMSpawnerFromSpawner_CustomMissingCommand(t *testing.T) {
	row := &ent.Spawner{
		AdapterType: "custom",
		Command:     "",
	}
	got, err := pipeline.NewLLMSpawnerFromSpawner(row)
	require.Error(t, err)
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), "custom adapter requires spawner.command")
}

func TestNewLLMSpawnerFromSpawner_UnknownType(t *testing.T) {
	row := &ent.Spawner{AdapterType: "fancy-new-llm"}
	got, err := pipeline.NewLLMSpawnerFromSpawner(row)
	require.Error(t, err)
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), "unknown adapter_type")
}
