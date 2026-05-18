// Package adapters implements GET/PUT /api/settings/adapters and GET /api/adapters.
package adapters

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/lx-wnk/agent-dashboard/server/internal/apierr"
	"github.com/lx-wnk/agent-dashboard/server/internal/config"
)

// adapterMeta describes one available LLM adapter and its config requirements.
type adapterMeta struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	ConfigKeys  []configKeyDoc `json:"configKeys"`
}

type configKeyDoc struct {
	Key      string `json:"key"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
	Note     string `json:"note,omitempty"`
}

var availableAdapters = []adapterMeta{
	{
		Name:        "claude",
		Description: "Default Claude CLI adapter — spawns a detached claude process per stage.",
		ConfigKeys:  []configKeyDoc{},
	},
	{
		Name:        "ollama",
		Description: "Ollama HTTP adapter — calls the /api/chat endpoint synchronously.",
		ConfigKeys: []configKeyDoc{
			{Key: "adapters.ollama.host", Type: "string", Required: false, Note: "Ollama base URL, default http://localhost:11434"},
			{Key: "adapters.ollama.default_model", Type: "string", Required: false, Note: "Model name passed when LLMSpawnArgs.Model is empty"},
		},
	},
	{
		Name:        "openai",
		Description: "OpenAI-compatible HTTP adapter — calls /chat/completions on any OpenAI-compatible endpoint.",
		ConfigKeys: []configKeyDoc{
			{Key: "adapters.openai.base_url", Type: "string", Required: false, Note: "Base URL, default https://api.openai.com/v1"},
			{Key: "adapters.openai.api_key_env", Type: "string", Required: true, Note: "Name of the env var holding the API key"},
			{Key: "adapters.openai.default_model", Type: "string", Required: false, Note: "Model name used when LLMSpawnArgs.Model is empty"},
		},
	},
	{
		Name:        "custom",
		Description: "Custom command adapter — runs DASHBOARD_SPAWN_COMMAND, passes LLMSpawnArgs as JSON on stdin, reads LLMSpawnResult from stdout.",
		ConfigKeys: []configKeyDoc{
			{Key: "DASHBOARD_SPAWN_COMMAND", Type: "env", Required: true, Note: "Path to the executable that implements the LLMSpawner contract"},
		},
	},
}

// Handler serves the /api/adapters and /api/settings/adapters endpoints.
type Handler struct {
	mu      sync.RWMutex
	cfg     *config.AdapterConfig
	cfgFile string // path to the JSON config file, or "" if not loaded from a file
}

// NewHandler creates a Handler backed by the given AdapterConfig pointer.
// cfgFile is the path to the JSON config file that was loaded at startup; it is
// used by setCurrent to persist the new default adapter to disk so the change
// survives a server restart. Pass "" if no config file is in use.
//
// The pointer must remain valid for the server lifetime; in-memory updates via
// PUT /api/settings/adapters will mutate through it.
func NewHandler(cfg *config.AdapterConfig, cfgFile string) *Handler {
	return &Handler{cfg: cfg, cfgFile: cfgFile}
}

// Mount registers all adapter routes on r.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/api/adapters", apierr.ErrorMiddleware(h.list))
	r.Get("/api/adapters/current", apierr.ErrorMiddleware(h.getCurrent))
	r.Post("/api/adapters/current", apierr.ErrorMiddleware(h.setCurrent))
	r.Get("/api/settings/adapters", apierr.ErrorMiddleware(h.getConfig))
	r.Put("/api/settings/adapters", apierr.ErrorMiddleware(h.putConfig))
}

// list returns the static catalogue of available adapters with their config requirements.
func (h *Handler) list(w http.ResponseWriter, _ *http.Request) error {
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(availableAdapters)
}

// getCurrent returns the name of the currently active adapter.
func (h *Handler) getCurrent(w http.ResponseWriter, _ *http.Request) error {
	h.mu.RLock()
	active := h.cfg.Default
	h.mu.RUnlock()
	if active == "" {
		active = "claude"
	}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(map[string]string{"adapter": active})
}

// setCurrent updates the active adapter name in the in-memory config and, when a
// config file is configured, persists the new default to disk so the change
// survives a server restart.
//
// Body: {"adapter":"ollama","config":{...optional full AdapterConfig...}}
func (h *Handler) setCurrent(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Adapter string                `json:"adapter"`
		Config  *config.AdapterConfig `json:"config,omitempty"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body); err != nil {
		return apierr.NewAppError(http.StatusBadRequest, "invalid JSON body")
	}
	if body.Adapter == "" {
		return apierr.NewAppError(http.StatusBadRequest, "adapter is required")
	}
	// Validate against known adapter names.
	known := false
	for _, a := range availableAdapters {
		if a.Name == body.Adapter {
			known = true
			break
		}
	}
	if !known {
		return apierr.NewAppError(http.StatusBadRequest, "unknown adapter: "+body.Adapter)
	}

	h.mu.Lock()
	h.cfg.Default = body.Adapter
	// If the caller also supplied a full config block, apply it.
	if body.Config != nil {
		h.cfg.Ollama = body.Config.Ollama
		h.cfg.OpenAI = body.Config.OpenAI
		if body.Config.Stages != nil {
			h.cfg.Stages = body.Config.Stages
		}
	}
	snapshot := *h.cfg
	h.mu.Unlock()

	// Persist to disk so the new default survives a server restart.
	h.tryPersist(snapshot)

	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(map[string]any{"adapter": snapshot.Default, "restartRequired": true})
}

// tryPersist writes snapshot to disk when a cfgFile is configured.
// snapshot must be a value copy (not a pointer) taken while h.mu is held,
// so the caller releases the lock before this function writes to disk.
// Persistence failures are non-fatal: the in-memory change is already applied.
func (h *Handler) tryPersist(snapshot config.AdapterConfig) {
	if h.cfgFile != "" {
		if err := persistAdapterConfig(h.cfgFile, snapshot); err != nil {
			slog.Warn("adapters: failed to persist config to disk", "file", h.cfgFile, "err", err)
		}
	}
}

// persistAdapterConfig reads the existing JSON config file (if present), updates
// the adapters key, and writes it back atomically. If the file does not exist yet
// it is created with a minimal structure containing only the adapters section.
// The read-before-write preserves any keys we do not own (e.g. scanner config).
func persistAdapterConfig(cfgFile string, ac config.AdapterConfig) error {
	// Read existing file content into a generic map so we don't lose other keys.
	raw := map[string]any{}
	if data, err := os.ReadFile(cfgFile); err == nil {
		// Ignore unmarshal errors — we'll just overwrite with a fresh map.
		_ = json.Unmarshal(data, &raw)
	}

	// Merge: update only the adapters sub-tree.
	adaptersMap := map[string]any{
		"default": ac.Default,
	}
	if ac.Ollama.Host != "" || ac.Ollama.DefaultModel != "" {
		adaptersMap["ollama"] = map[string]any{
			"host":          ac.Ollama.Host,
			"default_model": ac.Ollama.DefaultModel,
		}
	}
	if ac.OpenAI.BaseURL != "" || ac.OpenAI.APIKeyEnv != "" || ac.OpenAI.DefaultModel != "" {
		adaptersMap["openai"] = map[string]any{
			"base_url":      ac.OpenAI.BaseURL,
			"api_key_env":   ac.OpenAI.APIKeyEnv,
			"default_model": ac.OpenAI.DefaultModel,
		}
	}
	if len(ac.Stages) > 0 {
		stages := map[string]any{}
		for k, v := range ac.Stages {
			stages[k] = v
		}
		adaptersMap["stages"] = stages
	}
	raw["adapters"] = adaptersMap

	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cfgFile, data, 0o600)
}

// getConfig returns the full AdapterConfig as JSON.
func (h *Handler) getConfig(w http.ResponseWriter, _ *http.Request) error {
	h.mu.RLock()
	snapshot := *h.cfg
	h.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(&snapshot)
}

// putConfig replaces the full AdapterConfig from the request body in memory
// and, when a config file is configured, persists it to disk so the change
// survives a server restart.
func (h *Handler) putConfig(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	var incoming config.AdapterConfig
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		return apierr.NewAppError(http.StatusBadRequest, "invalid JSON body")
	}
	if incoming.Default != "" {
		known := false
		for _, a := range availableAdapters {
			if a.Name == incoming.Default {
				known = true
				break
			}
		}
		if !known {
			return apierr.NewAppError(http.StatusBadRequest, "unknown adapter: "+incoming.Default)
		}
	}
	h.mu.Lock()
	*h.cfg = incoming
	snapshot := *h.cfg
	h.mu.Unlock()

	// Persist to disk so the config survives a server restart.
	h.tryPersist(snapshot)

	type response struct {
		config.AdapterConfig
		RestartRequired bool `json:"restartRequired"`
	}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(response{AdapterConfig: snapshot, RestartRequired: true})
}
