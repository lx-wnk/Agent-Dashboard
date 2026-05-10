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

// decode reads JSON from r.Body into v.
func decode[T any](r *http.Request) (T, error) {
	var v T
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		return v, fmt.Errorf("%w: %w", ErrBadRequest, err)
	}
	return v, nil
}
