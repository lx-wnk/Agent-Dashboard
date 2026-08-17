package llmadapter

// AdapterMeta describes one available LLM adapter and its configuration
// requirements. The catalog is the single source of truth consumed by both
// the pipeline factory (NewLLMSpawnerFromSpawner) and the HTTP handler that
// exposes adapter metadata to the UI.
type AdapterMeta struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	ConfigKeys  []ConfigKeyDoc `json:"configKeys"`
}

// ConfigKeyDoc describes a single configurable key for an adapter.
// Type is one of: "string", "env". Required signals UI hard validation.
type ConfigKeyDoc struct {
	Key      string `json:"key"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
	Note     string `json:"note,omitempty"`
}

// AvailableAdapters is the canonical catalog of LLM adapter types.
// adapter_factory.go switches on the adapter Name field below to build the
// appropriate LLMSpawner from a spawners-table row.
var AvailableAdapters = []AdapterMeta{
	{
		Name:        "claude",
		Description: "Default Claude CLI adapter — spawns a detached claude process per stage.",
		ConfigKeys:  []ConfigKeyDoc{},
	},
	{
		Name:        "ollama",
		Description: "Ollama HTTP adapter — calls the /api/chat endpoint synchronously.",
		ConfigKeys: []ConfigKeyDoc{
			{Key: "host", Type: "string", Required: false, Note: "Ollama base URL, default http://localhost:11434"},
			{Key: "default_model", Type: "string", Required: false, Note: "Model name passed when LLMSpawnArgs.Model is empty"},
		},
	},
	{
		Name:        "openai",
		Description: "OpenAI-compatible HTTP adapter — calls /chat/completions on any OpenAI-compatible endpoint.",
		ConfigKeys: []ConfigKeyDoc{
			{Key: "base_url", Type: "string", Required: false, Note: "Base URL, default https://api.openai.com/v1"},
			{Key: "api_key_env", Type: "string", Required: true, Note: "Name of the env var holding the API key"},
			{Key: "default_model", Type: "string", Required: false, Note: "Model name used when LLMSpawnArgs.Model is empty"},
		},
	},
	{
		Name:        "custom",
		Description: "Custom command adapter — runs the spawner row's `command`, passes LLMSpawnArgs as JSON on stdin, reads LLMSpawnResult from stdout.",
		ConfigKeys:  []ConfigKeyDoc{},
	},
	{
		Name:        "acp",
		Description: "Agent Client Protocol adapter — drives an ACP agent for the whole stage. The configured agent's `default` mode must be its ask-first mode for the gate to mean anything; ACP does not require this of an arbitrary agent. Permission requests are denied until the approval gate is wired, so use it for stages that need no approvals.",
		ConfigKeys: []ConfigKeyDoc{
			{Key: "command", Type: "string", Required: false, Note: "Agent executable, default npx"},
			{Key: "args", Type: "string", Required: false, Note: "Space-separated arguments, default -y @agentclientprotocol/claude-agent-acp@0.68.0"},
		},
	},
}
