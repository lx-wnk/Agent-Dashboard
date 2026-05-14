package config

import "os"

// AdapterConfig selects which LLM adapter to use per stage.
type AdapterConfig struct {
	Default string            `koanf:"default"` // "claude" | "openai" | "ollama" | "custom"
	Stages  map[string]string `koanf:"stages"`  // stage name → adapter name
	Ollama  OllamaConfig      `koanf:"ollama"`
	OpenAI  OpenAIConfig      `koanf:"openai"`
}

type OllamaConfig struct {
	Host         string `koanf:"host"`
	DefaultModel string `koanf:"default_model"`
}

type OpenAIConfig struct {
	BaseURL      string `koanf:"base_url"`
	APIKeyEnv    string `koanf:"api_key_env"`
	DefaultModel string `koanf:"default_model"`
}

// AdapterForStage returns the configured adapter name for the given stage,
// falling back to Default, then "claude".
func (a AdapterConfig) AdapterForStage(stage string) string {
	if name, ok := a.Stages[stage]; ok && name != "" {
		return name
	}
	if a.Default != "" {
		return a.Default
	}
	return "claude"
}

// SpawnCommandFromEnv returns the value of DASHBOARD_SPAWN_COMMAND, or "".
func SpawnCommandFromEnv() string { return os.Getenv("DASHBOARD_SPAWN_COMMAND") }
