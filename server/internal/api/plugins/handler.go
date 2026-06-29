package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/lx-wnk/agent-dashboard/server/internal/apierr"
	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
	"github.com/lx-wnk/agent-dashboard/server/internal/pluginlifecycle"
	"github.com/lx-wnk/agent-dashboard/server/internal/pluginsctl"
	"github.com/lx-wnk/agent-dashboard/server/internal/pluginsettings"
)

// Controller is the behaviour the handler needs from pluginsctl; faked in tests.
type Controller interface {
	List() ([]pluginsctl.PluginState, error)
}

// Handler serves the plugin listing endpoint under /api/settings/plugins.
type Handler struct {
	ctl Controller
}

// New creates a Handler backed by the given controller.
func New(ctl Controller) *Handler {
	return &Handler{ctl: ctl}
}

// Mount registers the read-only plugin listing on r. Enable/disable is handled
// live by the lifecycle endpoints (/api/plugins/{id}/activate|deactivate).
func (h *Handler) Mount(r chi.Router) {
	r.Get("/api/settings/plugins", apierr.ErrorMiddleware(h.list))
}

// pluginInfo is intentionally a narrow DTO. Do NOT add Entry/Descriptor fields —
// Descriptor.Env may carry plugin auth secrets and BaseURL must never be exposed
// (F028: leaks internal plugin address — clients must not proxy directly to plugins).
type pluginInfo struct {
	ID           string   `json:"id"`
	Capabilities []string `json:"capabilities"`
	Enabled      bool     `json:"enabled"`
	Healthy      bool     `json:"healthy"`
	AuthProvider bool     `json:"authProvider"`
}

func (h *Handler) list(w http.ResponseWriter, _ *http.Request) error {
	states, err := h.ctl.List()
	if err != nil {
		return fmt.Errorf("plugins.list: %w", err)
	}
	out := make([]pluginInfo, 0, len(states))
	for _, s := range states {
		caps := s.Capabilities
		if caps == nil {
			caps = []string{}
		}
		out = append(out, pluginInfo{
			ID:           s.ID,
			Capabilities: caps,
			Enabled:      s.Enabled,
			Healthy:      s.Healthy,
			AuthProvider: s.AuthProvider,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache") // F034: prevents stale plugin list after restart
	return json.NewEncoder(w).Encode(out)
}

// PluginView is the narrow lifecycle DTO served under /api/plugins. As with
// pluginInfo it intentionally omits BaseURL/Addr/Command/Env — plugin auth
// secrets and internal addresses must never reach the client (F028).
type PluginView struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Version         string   `json:"version"`
	State           string   `json:"state"` // discovered|inactive|active
	UpdateAvailable bool     `json:"updateAvailable"`
	Capabilities    []string `json:"capabilities"`
	HasSettings     bool     `json:"hasSettings"`
}

// controller is the lifecycle behaviour the handler needs; faked in tests and
// implemented by pluginlifecyclectl. Action is one of install|activate|
// deactivate|uninstall.
type controller interface {
	List(ctx context.Context) ([]PluginView, error)
	Transition(ctx context.Context, id, action string) (PluginView, error)
	GetSettings(ctx context.Context, id string) (schema []plugin.SettingField, values map[string]string, err error)
	PutSettings(ctx context.Context, id string, values map[string]string) error
}

// lifecycleActions is the closed set of accepted transition verbs.
var lifecycleActions = map[string]bool{
	"install":    true,
	"activate":   true,
	"deactivate": true,
	"uninstall":  true,
}

// LifecycleHandler serves the SP1 lifecycle + settings endpoints under
// /api/plugins. It is independent of the interim #230 enable/disable Handler.
type LifecycleHandler struct {
	ctl controller
}

// NewLifecycle creates a LifecycleHandler backed by the given controller.
func NewLifecycle(ctl controller) *LifecycleHandler { return &LifecycleHandler{ctl: ctl} }

// Mount registers the lifecycle + settings routes on r.
func (h *LifecycleHandler) Mount(r chi.Router) {
	r.Get("/api/plugins", apierr.ErrorMiddleware(h.list))
	r.Post("/api/plugins/{id}/{action}", apierr.ErrorMiddleware(h.transition))
	r.Get("/api/plugins/{id}/settings", apierr.ErrorMiddleware(h.getSettings))
	r.Put("/api/plugins/{id}/settings", apierr.ErrorMiddleware(h.putSettings))
}

// classify maps the controller sentinels to 400 and everything else to a plain
// 500 wrap.
func classify(err error, wrap string) error {
	if errors.Is(err, pluginlifecycle.ErrIllegalTransition) {
		return fmt.Errorf("%w: %s", apierr.ErrConflict, err.Error())
	}
	if errors.Is(err, pluginsctl.ErrUnknownPlugin) || errors.Is(err, pluginsctl.ErrInvalidAction) ||
		errors.Is(err, pluginsettings.ErrUnknownKey) {
		return fmt.Errorf("%w: %s", apierr.ErrBadRequest, err.Error())
	}
	return fmt.Errorf("%s: %w", wrap, err)
}

func (h *LifecycleHandler) list(w http.ResponseWriter, r *http.Request) error {
	views, err := h.ctl.List(r.Context())
	if err != nil {
		return fmt.Errorf("plugins.lifecycle.list: %w", err)
	}
	if views == nil {
		views = []PluginView{}
	}
	for i := range views {
		if views[i].Capabilities == nil {
			views[i].Capabilities = []string{}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	return json.NewEncoder(w).Encode(views)
}

func (h *LifecycleHandler) transition(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if id == "" {
		return fmt.Errorf("%w: plugin id is required", apierr.ErrBadRequest)
	}
	if !plugin.ValidID(id) {
		return fmt.Errorf("%w: invalid plugin id %q", apierr.ErrBadRequest, id)
	}
	action := chi.URLParam(r, "action")
	if !lifecycleActions[action] {
		return fmt.Errorf("%w: invalid action %q", apierr.ErrBadRequest, action)
	}
	view, err := h.ctl.Transition(r.Context(), id, action)
	if err != nil {
		return classify(err, "plugins.lifecycle.transition")
	}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(view)
}

type settingsResponse struct {
	Schema []plugin.SettingField `json:"schema"`
	Values map[string]string     `json:"values"`
}

func (h *LifecycleHandler) getSettings(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if id == "" {
		return fmt.Errorf("%w: plugin id is required", apierr.ErrBadRequest)
	}
	if !plugin.ValidID(id) {
		return fmt.Errorf("%w: invalid plugin id %q", apierr.ErrBadRequest, id)
	}
	schema, values, err := h.ctl.GetSettings(r.Context(), id)
	if err != nil {
		return classify(err, "plugins.lifecycle.getSettings")
	}
	if schema == nil {
		schema = []plugin.SettingField{}
	}
	if values == nil {
		values = map[string]string{}
	}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(settingsResponse{Schema: schema, Values: values})
}

func (h *LifecycleHandler) putSettings(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if id == "" {
		return fmt.Errorf("%w: plugin id is required", apierr.ErrBadRequest)
	}
	if !plugin.ValidID(id) {
		return fmt.Errorf("%w: invalid plugin id %q", apierr.ErrBadRequest, id)
	}
	var body struct {
		Values map[string]string `json:"values"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return fmt.Errorf("%w: invalid JSON", apierr.ErrBadRequest)
	}
	if err := h.ctl.PutSettings(r.Context(), id, body.Values); err != nil {
		return classify(err, "plugins.lifecycle.putSettings")
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
