package admin_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/api/admin"
)

type fakeValidator struct{ err error }

func (f fakeValidator) Validate(context.Context) error { return f.err }

func mount(h *admin.Handler) http.Handler {
	r := chi.NewRouter()
	h.Mount(r)
	return r
}

func TestRestartReturns202OnSuccess(t *testing.T) {
	triggered := make(chan struct{}, 1)
	h := admin.New(fakeValidator{}, "reexec", func() { triggered <- struct{}{} })
	rec := httptest.NewRecorder()
	mount(h).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/admin/restart", nil))

	require.Equal(t, http.StatusAccepted, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "restarting", body["status"])
	require.Equal(t, "reexec", body["mode"])
	select {
	case <-triggered:
	default:
		t.Fatal("expected restart trigger to fire")
	}
}

func TestRestartReturns409WhenValidatorFails(t *testing.T) {
	h := admin.New(fakeValidator{err: errors.New("auth would lock out")}, "reexec", func() { t.Fatal("must not trigger") })
	rec := httptest.NewRecorder()
	mount(h).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/admin/restart", nil))
	require.Equal(t, http.StatusConflict, rec.Code)
}
