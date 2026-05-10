package apikeys

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/lx-wnk/agent-dashboard/server/internal/api"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// Handler handles API key management routes.
type Handler struct {
	repo repo.ApiKeyRepo
}

// NewHandler creates an API key Handler.
func NewHandler(r repo.ApiKeyRepo) *Handler {
	return &Handler{repo: r}
}

// Wrap converts a handler-returns-error function to a chi-compatible HandlerFunc.
func Wrap(fn func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return api.ErrorMiddleware(api.HandlerFunc(fn))
}

// List returns all active API keys. Never includes key_hash or raw token.
// GET /api/settings/api-keys
func (h *Handler) List(w http.ResponseWriter, r *http.Request) error {
	keys, err := h.repo.List(r.Context())
	if err != nil {
		return fmt.Errorf("apikeys.List: %w", err)
	}
	type keyView struct {
		ID        string   `json:"id"`
		Name      string   `json:"name"`
		Scopes    []string `json:"scopes"`
		Active    bool     `json:"active"`
		CreatedAt string   `json:"created_at"`
	}
	out := make([]keyView, len(keys))
	for i, k := range keys {
		out[i] = keyView{
			ID:        k.ID,
			Name:      k.Name,
			Scopes:    k.Scopes,
			Active:    k.Active,
			CreatedAt: k.CreatedAt.Format("2006-01-02T15:04:05Z"),
		}
	}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(out)
}

// Create creates a new API key. Returns the raw token once — it is not stored.
// POST /api/settings/api-keys  body: {"name":"...","scopes":["tasks:read",...]}
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return fmt.Errorf("%w: invalid JSON", api.ErrBadRequest)
	}
	if body.Name == "" {
		return fmt.Errorf("%w: name is required", api.ErrBadRequest)
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Errorf("apikeys.Create: generate token: %w", err)
	}
	token := "mcp_" + base64.RawURLEncoding.EncodeToString(raw)

	sum := sha256.Sum256([]byte(token))
	hash := hex.EncodeToString(sum[:])

	key, err := h.repo.Create(r.Context(), body.Name, hash, body.Scopes)
	if err != nil {
		return fmt.Errorf("apikeys.Create: %w", err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	return json.NewEncoder(w).Encode(map[string]any{
		"id":         key.ID,
		"name":       key.Name,
		"scopes":     key.Scopes,
		"token":      token,
		"created_at": key.CreatedAt.Format("2006-01-02T15:04:05Z"),
	})
}

// Delete soft-deletes an API key by ID.
// DELETE /api/settings/api-keys/{id}
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if id == "" {
		return fmt.Errorf("%w: id is required", api.ErrBadRequest)
	}
	if err := h.repo.Delete(r.Context(), id); err != nil {
		return fmt.Errorf("apikeys.Delete: %w", err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
