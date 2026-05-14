package systemprompts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/api/systemprompts"
	"github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

const testJWTSecret = "test-secret-for-systemprompts"

// jwtMiddleware signs a JWT for "user-1" and injects it so handlers can call
// auth.PayloadFromContext. Mirrors the pattern used in presets/handler_test.go.
func jwtMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, err := auth.SignJWT(auth.JWTPayload{Sub: "user-1"}, testJWTSecret, 3600)
		if err != nil {
			http.Error(w, "test setup: sign jwt: "+err.Error(), http.StatusInternalServerError)
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), struct{ name string }{"jwt_for_test"}, tok))
		r.AddCookie(&http.Cookie{Name: "auth_token", Value: tok})
		auth.RequireAuth(testJWTSecret)(next).ServeHTTP(w, r)
	})
}

func setupHandler(t *testing.T) *chi.Mux {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })

	r := repo.NewSystemPromptRepo(bundle.Client)
	h := systemprompts.NewHandler(r)

	mux := chi.NewRouter()
	mux.Group(func(r chi.Router) {
		r.Use(jwtMiddleware)
		h.Mount(r)
	})
	return mux
}

func TestSystemPromptsHandler_List_EmptyReturnsArray(t *testing.T) {
	mux := setupHandler(t)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/settings/system-prompts", nil))
	require.Equal(t, http.StatusOK, w.Code)

	var list []any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&list))
	require.Empty(t, list)
}

func TestSystemPromptsHandler_Create_Valid(t *testing.T) {
	mux := setupHandler(t)

	body, _ := json.Marshal(map[string]any{
		"content":  "You are a helpful assistant.",
		"scope":    "global",
		"priority": 10,
	})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/settings/system-prompts", bytes.NewReader(body)))
	require.Equal(t, http.StatusCreated, w.Code)

	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Equal(t, "You are a helpful assistant.", resp["content"])
	require.Equal(t, "global", resp["scope"])
}

func TestSystemPromptsHandler_Create_MissingContent_Returns400(t *testing.T) {
	mux := setupHandler(t)

	body, _ := json.Marshal(map[string]any{"scope": "global"})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/settings/system-prompts", bytes.NewReader(body)))
	require.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Contains(t, resp["error"], "content")
}

func TestSystemPromptsHandler_Create_InvalidScope_Returns400(t *testing.T) {
	mux := setupHandler(t)

	body, _ := json.Marshal(map[string]any{
		"content": "Task-scoped prompt.",
		"scope":   "task",
	})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/settings/system-prompts", bytes.NewReader(body)))
	require.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Contains(t, resp["error"], "global")
}

func TestSystemPromptsHandler_Update_NotFound_Returns404(t *testing.T) {
	mux := setupHandler(t)

	content := "Updated content."
	body, _ := json.Marshal(map[string]any{"content": content})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/settings/system-prompts/non-existent-id", bytes.NewReader(body)))
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestSystemPromptsHandler_Delete_NotFound_Returns404(t *testing.T) {
	mux := setupHandler(t)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/settings/system-prompts/non-existent-id", nil))
	require.Equal(t, http.StatusNotFound, w.Code)
}
