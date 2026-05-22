package adapters

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestList_ReturnsCatalog verifies GET /api/adapters still returns the
// canonical adapter catalog.
func TestList_ReturnsCatalog(t *testing.T) {
	t.Parallel()

	r := chi.NewRouter()
	NewHandler().Mount(r)

	req := httptest.NewRequest(http.MethodGet, "/api/adapters", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("expected application/json content-type, got %q", got)
	}

	var catalog []map[string]any
	if err := json.NewDecoder(w.Body).Decode(&catalog); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if len(catalog) == 0 {
		t.Errorf("expected non-empty adapter catalog")
	}
}

// TestRetiredEndpoints_Return410 verifies that the four legacy write endpoints
// respond with HTTP 410 Gone and the documented migration body.
func TestRetiredEndpoints_Return410(t *testing.T) {
	t.Parallel()

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/adapters/current"},
		{http.MethodPost, "/api/adapters/current"},
		{http.MethodGet, "/api/settings/adapters"},
		{http.MethodPut, "/api/settings/adapters"},
	}

	r := chi.NewRouter()
	NewHandler().Mount(r)

	for _, tc := range cases {
		tc := tc
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusGone {
				t.Fatalf("expected 410, got %d: %s", w.Code, w.Body.String())
			}
			var body map[string]string
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["error"] != "endpoint retired" {
				t.Errorf("unexpected error key: %q", body["error"])
			}
			if body["message"] == "" || body["docs"] == "" {
				t.Errorf("expected message + docs keys, got: %+v", body)
			}
		})
	}
}
