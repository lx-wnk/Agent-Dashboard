package plugins_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/plugins"
	"github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
	"github.com/lx-wnk/agent-dashboard/server/internal/pluginlifecycle"
	"github.com/lx-wnk/agent-dashboard/server/internal/pluginsctl"
	"github.com/lx-wnk/agent-dashboard/server/internal/pluginsettings"
)

const testJWTSecret = "test-secret-plugins"

// stubController satisfies the trimmed Controller interface (List only).
type stubController struct {
	states []pluginsctl.PluginState
}

func (s stubController) List() ([]pluginsctl.PluginState, error) { return s.states, nil }

// fakeController stands in for *pluginsctl.Controller (list-only now).
type fakeController struct {
	states []pluginsctl.PluginState
}

func (f *fakeController) List() ([]pluginsctl.PluginState, error) { return f.states, nil }

// withAuth adds a valid JWT session cookie so auth.RequireAuth passes.
func withAuth(t *testing.T, r *http.Request) *http.Request {
	t.Helper()
	token, err := auth.SignJWT(auth.JWTPayload{Sub: "user-1", Login: "testuser", IsAdmin: true}, testJWTSecret, 3600)
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	r.AddCookie(&http.Cookie{Name: "auth_token", Value: token})
	return r
}

func mount(t *testing.T, ctl plugins.Controller) http.Handler {
	t.Helper()
	h := plugins.New(ctl)
	r := chi.NewRouter()
	r.Use(auth.RequireAuth(testJWTSecret))
	h.Mount(r)
	return r
}

// TestList_ShapeAndLeakGuard verifies the list DTO carries exactly the allowed
// keys (incl. enabled/healthy/authProvider) and never leaks baseURL or env.
func TestList_ShapeAndLeakGuard(t *testing.T) {
	ctl := &fakeController{states: []pluginsctl.PluginState{{
		ID:           "fake-plugin",
		Capabilities: []string{"route_extension"},
		Enabled:      false,
		Healthy:      true,
		AuthProvider: false,
	}}}

	req := withAuth(t, httptest.NewRequest(http.MethodGet, "/api/settings/plugins", nil))
	rr := httptest.NewRecorder()
	mount(t, ctl).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	rawBody := rr.Body.String()

	var items []map[string]any
	if err := json.Unmarshal([]byte(rawBody), &items); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(items))
	}
	item := items[0]

	for _, required := range []string{"id", "capabilities", "enabled", "healthy", "authProvider"} {
		if _, ok := item[required]; !ok {
			t.Errorf("response item missing required key %q", required)
		}
	}
	for _, forbidden := range []string{"env", "baseURL", "descriptor", "addr", "command", "version"} {
		if _, ok := item[forbidden]; ok {
			t.Errorf("response item must not contain key %q (F028/F034 leak guard)", forbidden)
		}
	}
	if len(item) != 5 {
		t.Errorf("response item has %d keys, want exactly 5; got: %v", len(item), item)
	}

	if item["id"] != "fake-plugin" {
		t.Errorf("id: got %v, want fake-plugin", item["id"])
	}
	if item["enabled"] != false || item["healthy"] != true || item["authProvider"] != false {
		t.Errorf("flags wrong: %v", item)
	}
	caps, _ := item["capabilities"].([]any)
	if len(caps) != 1 || caps[0] != "route_extension" {
		t.Errorf("capabilities: got %v", caps)
	}

	if strings.Contains(rawBody, "127.0.0.1") || strings.Contains(rawBody, "SUPER_SECRET_TOKEN") {
		t.Errorf("response body leaked internal plugin data: %s", rawBody)
	}
}

// TestPluginsEnabledPatchRouteRemoved asserts the interim PATCH route is gone.
func TestPluginsEnabledPatchRouteRemoved(t *testing.T) {
	r := chi.NewRouter()
	plugins.New(stubController{}).Mount(r)

	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"enabled":true}`)
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/api/settings/plugins-enabled/voice", body))

	// chi returns 405 when the path exists but the method doesn't, or 404 when
	// neither path nor method matches — both confirm the route is absent.
	if rec.Code != http.StatusMethodNotAllowed && rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 or 405, got %d", rec.Code)
	}
}

// fakeLifecycle stands in for *pluginlifecyclectl.Controller.
type fakeLifecycle struct {
	views        []plugins.PluginView
	transition   plugins.PluginView
	transErr     error
	gotID        string
	gotAction    string
	schema       []plugin.SettingField
	values       map[string]string
	getErr       error
	putErr       error
	putGotID     string
	putGotValues map[string]string
}

func (f *fakeLifecycle) List(context.Context) ([]plugins.PluginView, error) { return f.views, nil }

func (f *fakeLifecycle) Transition(_ context.Context, id, action string) (plugins.PluginView, error) {
	f.gotID, f.gotAction = id, action
	if f.transErr != nil {
		return plugins.PluginView{}, f.transErr
	}
	return f.transition, nil
}

func (f *fakeLifecycle) GetSettings(_ context.Context, id string) ([]plugin.SettingField, map[string]string, error) {
	f.gotID = id
	if f.getErr != nil {
		return nil, nil, f.getErr
	}
	return f.schema, f.values, nil
}

func (f *fakeLifecycle) PutSettings(_ context.Context, id string, values map[string]string) error {
	f.putGotID, f.putGotValues = id, values
	return f.putErr
}

func mountLifecycle(t *testing.T, ctl *fakeLifecycle) http.Handler {
	t.Helper()
	h := plugins.NewLifecycle(ctl)
	r := chi.NewRouter()
	r.Use(auth.RequireAuth(testJWTSecret))
	h.MountList(r)
	r.Group(func(r chi.Router) {
		h.Mount(r)
	})
	return r
}

// TestLifecycleList_NonAdminJWTAllowed verifies GET /api/plugins is accessible to
// any authenticated user, not only admins.
func TestLifecycleList_NonAdminJWTAllowed(t *testing.T) {
	h := plugins.NewLifecycle(&fakeLifecycle{views: []plugins.PluginView{}})
	r := chi.NewRouter()
	r.Use(auth.RequireAuth(testJWTSecret))
	h.MountList(r)

	nonAdminToken, err := auth.SignJWT(auth.JWTPayload{Sub: "u2", Login: "reader", IsAdmin: false}, testJWTSecret, 3600)
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/plugins", nil)
	req.AddCookie(&http.Cookie{Name: "auth_token", Value: nonAdminToken})
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for non-admin list, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestLifecycleList_ShapeAndLeakGuard verifies the lifecycle DTO carries exactly
// the documented keys and never leaks BaseURL/Env/Addr/Command.
func TestLifecycleList_ShapeAndLeakGuard(t *testing.T) {
	ctl := &fakeLifecycle{views: []plugins.PluginView{{
		ID:              "fake-plugin",
		Name:            "Fake",
		Version:         "1.2.3",
		State:           "active",
		UpdateAvailable: true,
		Capabilities:    []string{"route_extension"},
		HasSettings:     true,
	}}}

	req := withAuth(t, httptest.NewRequest(http.MethodGet, "/api/plugins", nil))
	rr := httptest.NewRecorder()
	mountLifecycle(t, ctl).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var items []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &items); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(items))
	}
	item := items[0]
	for _, required := range []string{"id", "name", "version", "state", "updateAvailable", "healthy", "capabilities", "hasSettings"} {
		if _, ok := item[required]; !ok {
			t.Errorf("response item missing required key %q", required)
		}
	}
	for _, forbidden := range []string{"env", "baseURL", "addr", "command", "descriptor"} {
		if _, ok := item[forbidden]; ok {
			t.Errorf("response item must not contain key %q (F028 leak guard)", forbidden)
		}
	}
	if len(item) != 8 {
		t.Errorf("response item has %d keys, want exactly 8; got: %v", len(item), item)
	}
	if item["id"] != "fake-plugin" || item["state"] != "active" || item["updateAvailable"] != true || item["hasSettings"] != true {
		t.Errorf("fields wrong: %v", item)
	}
}

func TestLifecycleTransition_ActivateReturnsState(t *testing.T) {
	ctl := &fakeLifecycle{transition: plugins.PluginView{ID: "p1", State: "active", Capabilities: []string{}}}
	req := withAuth(t, httptest.NewRequest(http.MethodPost, "/api/plugins/p1/activate", nil))
	rr := httptest.NewRecorder()
	mountLifecycle(t, ctl).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if ctl.gotID != "p1" || ctl.gotAction != "activate" {
		t.Errorf("controller got id=%q action=%q", ctl.gotID, ctl.gotAction)
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["state"] != "active" {
		t.Errorf("state: got %v, want active", resp["state"])
	}
}

func TestLifecycleTransition_UnknownID_400(t *testing.T) {
	ctl := &fakeLifecycle{transErr: fmt.Errorf("ctl: %w: %q", pluginsctl.ErrUnknownPlugin, "nope")}
	req := withAuth(t, httptest.NewRequest(http.MethodPost, "/api/plugins/nope/activate", nil))
	rr := httptest.NewRecorder()
	mountLifecycle(t, ctl).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestLifecycleTransition_InvalidAction_400(t *testing.T) {
	ctl := &fakeLifecycle{}
	req := withAuth(t, httptest.NewRequest(http.MethodPost, "/api/plugins/p1/frobnicate", nil))
	rr := httptest.NewRecorder()
	mountLifecycle(t, ctl).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if ctl.gotAction != "" {
		t.Errorf("invalid action must not reach controller, got %q", ctl.gotAction)
	}
}

func TestLifecycleTransition_IllegalTransition_409(t *testing.T) {
	ctl := &fakeLifecycle{transErr: fmt.Errorf("%w: p1 already installed", pluginlifecycle.ErrIllegalTransition)}
	req := withAuth(t, httptest.NewRequest(http.MethodPost, "/api/plugins/p1/install", nil))
	rr := httptest.NewRecorder()
	mountLifecycle(t, ctl).ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestLifecycleTransition_UpdateAccepted(t *testing.T) {
	ctl := &fakeLifecycle{transition: plugins.PluginView{ID: "p1", State: "active", Capabilities: []string{}}}
	req := withAuth(t, httptest.NewRequest(http.MethodPost, "/api/plugins/p1/update", nil))
	rr := httptest.NewRecorder()
	mountLifecycle(t, ctl).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for update action, got %d: %s", rr.Code, rr.Body.String())
	}
	if ctl.gotAction != "update" {
		t.Errorf("controller action: got %q, want update", ctl.gotAction)
	}
}

func TestLifecycleTransition_UpdateIllegalTransition_409(t *testing.T) {
	ctl := &fakeLifecycle{transErr: fmt.Errorf("%w: p1 not installed", pluginlifecycle.ErrIllegalTransition)}
	req := withAuth(t, httptest.NewRequest(http.MethodPost, "/api/plugins/p1/update", nil))
	rr := httptest.NewRecorder()
	mountLifecycle(t, ctl).ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409 for illegal update, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestLifecycleTransition_HookFailure_500(t *testing.T) {
	ctl := &fakeLifecycle{transErr: errors.New("activate hook: connection refused")}
	req := withAuth(t, httptest.NewRequest(http.MethodPost, "/api/plugins/p1/activate", nil))
	rr := httptest.NewRecorder()
	mountLifecycle(t, ctl).ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestLifecycleGetSettings_MasksSecrets(t *testing.T) {
	ctl := &fakeLifecycle{
		schema: []plugin.SettingField{
			{Key: "endpoint", Type: "url", Label: "Endpoint"},
			{Key: "token", Type: "string", Label: "Token", Secret: true},
		},
		values: map[string]string{"endpoint": "https://x", "token": pluginsettingsMasked},
	}
	req := withAuth(t, httptest.NewRequest(http.MethodGet, "/api/plugins/p1/settings", nil))
	rr := httptest.NewRecorder()
	mountLifecycle(t, ctl).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Schema []plugin.SettingField `json:"schema"`
		Values map[string]string     `json:"values"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Schema) != 2 {
		t.Fatalf("expected 2 schema fields, got %d", len(resp.Schema))
	}
	if resp.Values["token"] != pluginsettingsMasked {
		t.Errorf("secret token must be masked, got %q", resp.Values["token"])
	}
	if resp.Values["endpoint"] != "https://x" {
		t.Errorf("non-secret value wrong: %q", resp.Values["endpoint"])
	}
}

func TestLifecyclePutSettings_Persists(t *testing.T) {
	ctl := &fakeLifecycle{}
	body := `{"values":{"endpoint":"https://y","token":"new-secret"}}`
	req := withAuth(t, httptest.NewRequest(http.MethodPut, "/api/plugins/p1/settings", strings.NewReader(body)))
	rr := httptest.NewRecorder()
	mountLifecycle(t, ctl).ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rr.Code, rr.Body.String())
	}
	if ctl.putGotID != "p1" {
		t.Errorf("put got id=%q", ctl.putGotID)
	}
	if ctl.putGotValues["endpoint"] != "https://y" || ctl.putGotValues["token"] != "new-secret" {
		t.Errorf("put values wrong: %v", ctl.putGotValues)
	}
}

func TestLifecyclePutSettings_UnknownKey_400(t *testing.T) {
	ctl := &fakeLifecycle{putErr: fmt.Errorf("%w: %q", pluginsettings.ErrUnknownKey, "bogus")}
	body := `{"values":{"bogus":"x"}}`
	req := withAuth(t, httptest.NewRequest(http.MethodPut, "/api/plugins/p1/settings", strings.NewReader(body)))
	rr := httptest.NewRecorder()
	mountLifecycle(t, ctl).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestLifecyclePutSettings_InvalidValue_400(t *testing.T) {
	ctl := &fakeLifecycle{putErr: fmt.Errorf("%w: field %q requires an integer", pluginsettings.ErrInvalidValue, "count")}
	body := `{"values":{"count":"abc"}}`
	req := withAuth(t, httptest.NewRequest(http.MethodPut, "/api/plugins/p1/settings", strings.NewReader(body)))
	rr := httptest.NewRecorder()
	mountLifecycle(t, ctl).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

const pluginsettingsMasked = pluginsettings.MaskedSentinel

func TestLifecycleTransition_MalformedID_400(t *testing.T) {
	ctl := &fakeLifecycle{}
	req := withAuth(t, httptest.NewRequest(http.MethodPost, "/api/plugins/My-PLUGIN/activate", nil))
	rr := httptest.NewRecorder()
	mountLifecycle(t, ctl).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed id, got %d: %s", rr.Code, rr.Body.String())
	}
	if ctl.gotID != "" {
		t.Errorf("malformed id must not reach controller, got %q", ctl.gotID)
	}
}

func TestLifecycleGetSettings_MalformedID_400(t *testing.T) {
	ctl := &fakeLifecycle{}
	req := withAuth(t, httptest.NewRequest(http.MethodGet, "/api/plugins/UPPER/settings", nil))
	rr := httptest.NewRecorder()
	mountLifecycle(t, ctl).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed id, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestLifecyclePutSettings_MalformedID_400(t *testing.T) {
	ctl := &fakeLifecycle{}
	body := `{"values":{}}`
	req := withAuth(t, httptest.NewRequest(http.MethodPut, "/api/plugins/BAD_ID/settings", strings.NewReader(body)))
	rr := httptest.NewRecorder()
	mountLifecycle(t, ctl).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed id, got %d: %s", rr.Code, rr.Body.String())
	}
}
