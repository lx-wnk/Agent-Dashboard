package config

import "os"

// AdapterConfig selects which LLM adapter to use per stage.
//
// Deprecated: AdapterConfig has been retired in favor of per-row Spawner rows
// in the spawners table (each with its own adapter_type + adapter_config).
// The dashboard reads adapter-config.json only on first boot so
// migrateAdapterConfigToSpawners can lift legacy entries into Spawner rows.
// All runtime adapter selection now flows through the per-task spawner
// resolution chain (task.spawner_id ?? project.default_spawner_id ??
// claude-default). The types remain so existing config files continue to
// unmarshal cleanly during migration.
type AdapterConfig struct {
	Default string            `koanf:"default"` // "claude" | "openai" | "ollama" | "custom"
	Stages  map[string]string `koanf:"stages"`  // stage name → adapter name
	Ollama  OllamaConfig      `koanf:"ollama"`
	OpenAI  OpenAIConfig      `koanf:"openai"`
}

// OllamaConfig is part of the deprecated AdapterConfig — see AdapterConfig doc.
//
// Deprecated: kept only for the boot migration. Adapter-specific config now
// lives in the spawners.adapter_config JSON column.
type OllamaConfig struct {
	Host         string `koanf:"host"`
	DefaultModel string `koanf:"default_model"`
}

// OpenAIConfig is part of the deprecated AdapterConfig — see AdapterConfig doc.
//
// Deprecated: kept only for the boot migration. Adapter-specific config now
// lives in the spawners.adapter_config JSON column.
type OpenAIConfig struct {
	BaseURL      string `koanf:"base_url"`
	APIKeyEnv    string `koanf:"api_key_env"`
	DefaultModel string `koanf:"default_model"`
}

// SpawnCommandFromEnv returns the value of DASHBOARD_SPAWN_COMMAND, or "".
//
// Deprecated: DASHBOARD_SPAWN_COMMAND no longer has any runtime effect. The
// boot migration reads this once to seed an "imported-custom" Spawner row;
// going forward custom spawners are configured via the spawners table /
// /api/spawners REST endpoint.
func SpawnCommandFromEnv() string { return os.Getenv("DASHBOARD_SPAWN_COMMAND") }
