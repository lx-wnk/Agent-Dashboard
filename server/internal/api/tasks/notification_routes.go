package tasks

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/lx-wnk/agent-dashboard/server/internal/apierr"
)

var validEventTypes = map[string]bool{
	"on_hold":           true,
	"approval_needed":   true,
	"completed":         true,
	"failed":            true,
	"budget_exceeded":   true,
	"iteration_warning": true,
}

const notifPrefPrefix = "notif:pref:"
const notifCfgPrefix = "notif:config:"

type notifPref struct {
	EventType string   `json:"eventType"`
	Channels  []string `json:"channels"`
	Enabled   bool     `json:"enabled"`
}

func (h *Handler) listNotificationPreferences(w http.ResponseWriter, r *http.Request) error {
	all, err := h.cfgRepo.GetAll(r.Context())
	if err != nil {
		return fmt.Errorf("notif_prefs.list: %w", err)
	}
	prefs := make([]notifPref, 0)
	for k, v := range all {
		if !strings.HasPrefix(k, notifPrefPrefix) {
			continue
		}
		et := strings.TrimPrefix(k, notifPrefPrefix)
		var p notifPref
		if err2 := json.Unmarshal([]byte(v), &p); err2 != nil {
			continue
		}
		p.EventType = et
		prefs = append(prefs, p)
	}
	return jsonReply(w, http.StatusOK, prefs)
}

func (h *Handler) putNotificationPreference(w http.ResponseWriter, r *http.Request) error {
	eventType := chi.URLParam(r, "eventType")
	if !validEventTypes[eventType] {
		return apierr.NewAppError(http.StatusBadRequest, "unknown eventType")
	}
	var body struct {
		Channels []string `json:"channels"`
		Enabled  *bool    `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return apierr.NewAppError(http.StatusBadRequest, "invalid JSON body")
	}
	if body.Channels == nil {
		return apierr.NewAppError(http.StatusBadRequest, "channels must be an array")
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	pref := notifPref{EventType: eventType, Channels: body.Channels, Enabled: enabled}
	encoded, _ := json.Marshal(pref)
	if err := h.cfgRepo.Set(r.Context(), notifPrefPrefix+eventType, string(encoded)); err != nil {
		return fmt.Errorf("notif_prefs.put: %w", err)
	}
	return jsonReply(w, http.StatusOK, pref)
}

func (h *Handler) getNotificationConfig(w http.ResponseWriter, r *http.Request) error {
	all, err := h.cfgRepo.GetAll(r.Context())
	if err != nil {
		return fmt.Errorf("notif_config.get: %w", err)
	}
	result := make(map[string]string)
	for k, v := range all {
		if strings.HasPrefix(k, notifCfgPrefix) {
			result[strings.TrimPrefix(k, notifCfgPrefix)] = v
		}
	}
	return jsonReply(w, http.StatusOK, result)
}

func (h *Handler) putNotificationConfig(w http.ResponseWriter, r *http.Request) error {
	var updates map[string]any
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		return apierr.NewAppError(http.StatusBadRequest, "invalid JSON body")
	}
	for k, v := range updates {
		var val string
		switch tv := v.(type) {
		case string:
			val = tv
		case nil:
			val = ""
		default:
			val = fmt.Sprintf("%v", tv)
		}
		if err := h.cfgRepo.Set(r.Context(), notifCfgPrefix+k, val); err != nil {
			return fmt.Errorf("notif_config.put: %w", err)
		}
	}
	return h.getNotificationConfig(w, r)
}
