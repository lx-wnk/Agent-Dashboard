package api

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// encode writes v as JSON with the given status code.
func encode[T any](w http.ResponseWriter, status int, v T) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	return nil
}

// Encode writes v as JSON with the given status code. Used by sub-packages.
func Encode[T any](w http.ResponseWriter, status int, v T) error {
	return encode(w, status, v)
}
