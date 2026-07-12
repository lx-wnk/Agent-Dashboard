package apierr

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAppError(t *testing.T) {
	err := NewAppError(http.StatusTeapot, "I'm a teapot")
	if err.Error() != "I'm a teapot" {
		t.Errorf("Error() = %q, want %q", err.Error(), "I'm a teapot")
	}
	if err.Status != http.StatusTeapot {
		t.Errorf("Status = %d, want %d", err.Status, http.StatusTeapot)
	}
}

func TestErrorMiddleware_StatusMapping(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   map[string]string
	}{
		{
			name:       "AppError uses its own status and message",
			err:        NewAppError(http.StatusTeapot, "custom message"),
			wantStatus: http.StatusTeapot,
			wantBody:   map[string]string{"error": "custom message"},
		},
		{
			name:       "wrapped AppError still matches via errors.As",
			err:        fmt.Errorf("context: %w", NewAppError(http.StatusPaymentRequired, "pay up")),
			wantStatus: http.StatusPaymentRequired,
			wantBody:   map[string]string{"error": "pay up"},
		},
		{
			name:       "ErrNotFound maps to 404",
			err:        ErrNotFound,
			wantStatus: http.StatusNotFound,
			wantBody:   map[string]string{"error": "not found"},
		},
		{
			name:       "wrapped ErrNotFound maps to 404",
			err:        fmt.Errorf("lookup failed: %w", ErrNotFound),
			wantStatus: http.StatusNotFound,
			wantBody:   map[string]string{"error": "not found"},
		},
		{
			name:       "ErrConflict maps to 409",
			err:        ErrConflict,
			wantStatus: http.StatusConflict,
			wantBody:   map[string]string{"error": "conflict"},
		},
		{
			name:       "ErrBadRequest maps to 400",
			err:        ErrBadRequest,
			wantStatus: http.StatusBadRequest,
			wantBody:   map[string]string{"error": "bad request"},
		},
		{
			name:       "ErrForbidden maps to 403",
			err:        ErrForbidden,
			wantStatus: http.StatusForbidden,
			wantBody:   map[string]string{"error": "forbidden"},
		},
		{
			name:       "unmapped error maps to 500",
			err:        errors.New("boom"),
			wantStatus: http.StatusInternalServerError,
			wantBody:   map[string]string{"error": "internal server error"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := ErrorMiddleware(func(w http.ResponseWriter, r *http.Request) error {
				return tc.err
			})
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rec := httptest.NewRecorder()
			handler(rec, req)

			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal response body: %v", err)
			}
			if body["error"] != tc.wantBody["error"] {
				t.Errorf("body[error] = %q, want %q", body["error"], tc.wantBody["error"])
			}
		})
	}
}

func TestErrorMiddleware_NilError(t *testing.T) {
	handler := ErrorMiddleware(func(w http.ResponseWriter, r *http.Request) error {
		w.WriteHeader(http.StatusOK)
		return nil
	})
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", rec.Body.String())
	}
}

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteJSON(rec, http.StatusCreated, map[string]int{"id": 42})

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]int
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body["id"] != 42 {
		t.Errorf("body[id] = %d, want 42", body["id"])
	}
}

func TestJSONError(t *testing.T) {
	rec := httptest.NewRecorder()
	JSONError(rec, http.StatusBadRequest, "bad input")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body["error"] != "bad input" {
		t.Errorf("body[error] = %q, want %q", body["error"], "bad input")
	}
}

func TestSentinelErrorsIsRoundTrip(t *testing.T) {
	sentinels := []error{ErrNotFound, ErrConflict, ErrBadRequest, ErrForbidden}
	for _, sentinel := range sentinels {
		wrapped := fmt.Errorf("context: %w", sentinel)
		if !errors.Is(wrapped, sentinel) {
			t.Errorf("errors.Is failed to match wrapped %v", sentinel)
		}
	}
}
