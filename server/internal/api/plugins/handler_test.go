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
	"github.com/lx-wnk/agent-dashboard/server/internal/pluginsctl"
)

const testJWTSecret = "test-secret-plugins"

// fakeController stands in for *pluginsctl.Controller.
type fakeController struct {
	states  []pluginsctl.PluginState
	applied pluginsctl.Applied
	setErr  error
	gotID   string
	gotEn   bool
}

func (f *fakeController) List() ([]pluginsctl.PluginState, error) { return f.states, nil }

func (f *fakeController) SetEnabled(_ context.Context, id string, enable bool) (pluginsctl.Applied, error) {
	f.gotID, f.gotEn = id, enable
	if f.setErr != nil {
		return "", f.setErr
	}
	return f.applied, nil
}

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

func TestPatch_Live(t *testing.T) {
	ctl := &fakeController{applied: pluginsctl.AppliedLive}
	req := withAuth(t, httptest.NewRequest(http.MethodPatch, "/api/settings/plugins/voice-whisper", strings.NewReader(`{"enabled":true}`)))
	rr := httptest.NewRecorder()
	mount(t, ctl).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if ctl.gotID != "voice-whisper" || !ctl.gotEn {
		t.Errorf("controller got id=%q enabled=%v", ctl.gotID, ctl.gotEn)
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["id"] != "voice-whisper" || resp["enabled"] != true || resp["applied"] != "live" {
		t.Errorf("response wrong: %v", resp)
	}
}

func TestPatch_Restart(t *testing.T) {
	ctl := &fakeController{applied: pluginsctl.AppliedRestart}
	req := withAuth(t, httptest.NewRequest(http.MethodPatch, "/api/settings/plugins/github-auth", strings.NewReader(`{"enabled":true}`)))
	rr := httptest.NewRecorder()
	mount(t, ctl).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["applied"] != "restart" {
		t.Errorf("applied: got %v, want restart", resp["applied"])
	}
}

func TestPatch_UnknownID_400(t *testing.T) {
	// Wrap the sentinel so the handler's errors.Is check matches.
	ctl := &fakeController{setErr: fmt.Errorf("pluginsctl: %w: %q", pluginsctl.ErrUnknownPlugin, "nope")}
	req := withAuth(t, httptest.NewRequest(http.MethodPatch, "/api/settings/plugins/nope", strings.NewReader(`{"enabled":true}`)))
	rr := httptest.NewRecorder()
	mount(t, ctl).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPatch_LiveFailure_500(t *testing.T) {
	ctl := &fakeController{setErr: errors.New("start failed: connection refused")}
	req := withAuth(t, httptest.NewRequest(http.MethodPatch, "/api/settings/plugins/voice-whisper", strings.NewReader(`{"enabled":true}`)))
	rr := httptest.NewRecorder()
	mount(t, ctl).ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPatch_InvalidJSON_400(t *testing.T) {
	ctl := &fakeController{}
	req := withAuth(t, httptest.NewRequest(http.MethodPatch, "/api/settings/plugins/voice-whisper", strings.NewReader(`not json`)))
	rr := httptest.NewRecorder()
	mount(t, ctl).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}
