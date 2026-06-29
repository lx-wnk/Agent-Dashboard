package admin_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/api/admin"
	"github.com/lx-wnk/agent-dashboard/server/internal/auth"
)

const testAdminJWTSecret = "test-sp8-admin-secret"

// TestRestart_NonAdminUserGets403 verifies the security property that the restart
// endpoint must enforce: non-admin authenticated users receive 403. The router
// wraps AdminHandler.Mount with RequireAdminOrBypass (same pattern as spawners and
// system-prompts). This test builds that exact chi group to document the contract.
func TestRestart_NonAdminUserGets403(t *testing.T) {
	h := admin.New(fakeValidator{}, "reexec", func() { t.Fatal("must not trigger") })

	r := chi.NewRouter()
	r.Use(auth.RequireAuth(testAdminJWTSecret))
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAdminOrBypass(false)) // not bypass mode
		h.Mount(r)
	})

	token, err := auth.SignJWT(
		auth.JWTPayload{Sub: "u2", Login: "viewer", IsAdmin: false},
		testAdminJWTSecret, 3600,
	)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/restart", nil)
	req.AddCookie(&http.Cookie{Name: "auth_token", Value: token})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestRestart_BypassModeAllowsAnyRequest(t *testing.T) {
	triggered := make(chan struct{}, 1)
	h := admin.New(fakeValidator{}, "reexec", func() { triggered <- struct{}{} })

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAdminOrBypass(true)) // bypass = local single-user mode
		h.Mount(r)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/admin/restart", nil))
	require.Equal(t, http.StatusAccepted, rec.Code)
	select {
	case <-triggered:
	default:
		t.Fatal("trigger must fire in bypass mode")
	}
}
