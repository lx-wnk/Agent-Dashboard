// Package api provides HTTP handler utilities and middleware.
package api

import (
	"errors"
	"log/slog"
	"net/http"
)

// Sentinel errors — use errors.Is() to check.
var (
	ErrNotFound   = errors.New("not found")
	ErrConflict   = errors.New("conflict")
	ErrBadRequest = errors.New("bad request")
	ErrForbidden  = errors.New("forbidden")
)

// AppError carries an explicit HTTP status code.
// Use when a handler needs precise control over the response status.
type AppError struct {
	Status  int    `json:"-"`
	Message string `json:"error"`
}

// Error implements the error interface.
func (e *AppError) Error() string { return e.Message }

// NewAppError creates an AppError with the given status and message.
func NewAppError(status int, msg string) *AppError {
	return &AppError{Status: status, Message: msg}
}

// HandlerFunc is an HTTP handler that returns an error instead of writing it directly.
// Wrap with ErrorMiddleware to map errors to HTTP responses.
type HandlerFunc func(http.ResponseWriter, *http.Request) error

// ErrorMiddleware wraps a HandlerFunc and maps errors to HTTP responses.
// This is the single place where domain errors become HTTP status codes.
func ErrorMiddleware(next HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := next(w, r)
		if err == nil {
			return
		}
		var appErr *AppError
		switch {
		case errors.As(err, &appErr):
			writeJSON(w, appErr.Status, appErr)
		case errors.Is(err, ErrNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		case errors.Is(err, ErrConflict):
			writeJSON(w, http.StatusConflict, map[string]string{"error": "conflict"})
		case errors.Is(err, ErrBadRequest):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		case errors.Is(err, ErrForbidden):
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		default:
			slog.Error("unhandled handler error", "err", err, "path", r.URL.Path)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		}
	}
}

// writeJSON writes v as JSON with the given status. Encoding errors are discarded
// because headers are already sent at this point.
func writeJSON(w http.ResponseWriter, status int, v any) {
	_ = encode(w, status, v)
}
