package settings_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	settingsapi "github.com/lx-wnk/agent-dashboard/server/internal/api/settings"
	settingssvc "github.com/lx-wnk/agent-dashboard/server/internal/settings"
)

type memRepo struct{ m map[string]string }

func (r *memRepo) Get(_ context.Context, k string) (string, bool, error) {
	v, ok := r.m[k]
	return v, ok, nil
}
func (r *memRepo) Set(_ context.Context, k, v string) error             { r.m[k] = v; return nil }
func (r *memRepo) ListAll(_ context.Context) (map[string]string, error) { return r.m, nil }

func newRouter(t *testing.T) (http.Handler, *settingssvc.Service) {
	t.Helper()
	svc := settingssvc.New(&memRepo{m: map[string]string{}})
	require.NoError(t, svc.Load(context.Background()))
	h := settingsapi.NewHandler(svc)
	r := chi.NewRouter()
	h.Mount(r)
	return r, svc
}

func TestSettingsAPI_ListAndPatch(t *testing.T) {
	r, _ := newRouter(t)

	// GET returns definitions with current values
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var list []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	assert.NotEmpty(t, list)

	// each entry exposes value, default, and apply
	first := list[0]
	assert.Contains(t, first, "value")
	assert.Contains(t, first, "default")
	assert.Contains(t, first, "apply")

	// PATCH a restart key -> applied:"restart"
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/settings/spawn.rateLimit", strings.NewReader(`{"value":"9"}`))
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "restart", resp["applied"])
	assert.Equal(t, "9", resp["value"])

	// PATCH invalid value -> 400
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/settings/spawn.rateLimit", strings.NewReader(`{"value":"abc"}`))
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	// PATCH unknown key -> 400
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/settings/nope", strings.NewReader(`{"value":"1"}`))
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSettingsAPI_ManagedKeyHidden(t *testing.T) {
	r, _ := newRouter(t)

	// GET does not expose managed keys
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var list []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	for _, e := range list {
		assert.NotEqual(t, "plugins.enabled", e["key"], "managed key must not appear in list")
	}

	// PATCH a managed key -> 400
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/settings/plugins.enabled", strings.NewReader(`{"value":"foo"}`))
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
