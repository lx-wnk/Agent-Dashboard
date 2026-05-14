// Package adapters implements GET/PUT /api/settings/adapters and GET /api/adapters.
package adapters

import (
	"encoding/json"
	"net/http"
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
	mu  sync.RWMutex
	cfg *config.AdapterConfig
}

// NewHandler creates a Handler backed by the given AdapterConfig pointer.
// The pointer must remain valid for the server lifetime; in-memory updates via
// PUT /api/settings/adapters will mutate through it.
func NewHandler(cfg *config.AdapterConfig) *Handler {
	return &Handler{cfg: cfg}
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

// setCurrent updates the active adapter name in the in-memory config.
// The change is NOT persisted to disk — it is lost on server restart.
// Restart the server after updating the config file to apply changes permanently.
// Body: {"adapter":"ollama","config":{...optional full AdapterConfig...}}
func (h *Handler) setCurrent(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Adapter string                `json:"adapter"`
		Config  *config.AdapterConfig `json:"config,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
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
	activeAdapter := h.cfg.Default
	h.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(map[string]any{"adapter": activeAdapter, "restartRequired": true})
}

// getConfig returns the full AdapterConfig as JSON.
func (h *Handler) getConfig(w http.ResponseWriter, _ *http.Request) error {
	h.mu.RLock()
	snapshot := *h.cfg
	h.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(&snapshot)
}

// putConfig replaces the full AdapterConfig from the request body in memory.
// The change is NOT persisted to disk — it is lost on server restart.
func (h *Handler) putConfig(w http.ResponseWriter, r *http.Request) error {
	var incoming config.AdapterConfig
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		return apierr.NewAppError(http.StatusBadRequest, "invalid JSON body")
	}
	h.mu.Lock()
	*h.cfg = incoming
	snapshot := *h.cfg
	h.mu.Unlock()
	type response struct {
		config.AdapterConfig
		RestartRequired bool `json:"restartRequired"`
	}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(response{AdapterConfig: snapshot, RestartRequired: true})
}
