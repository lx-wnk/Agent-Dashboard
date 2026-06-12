package domainerr

import (
	"errors"
	"fmt"
	"testing"
)

func TestSentinelsNonNilAndDistinct(t *testing.T) {
	all := map[string]error{
		"ErrNotFound":   ErrNotFound,
		"ErrConflict":   ErrConflict,
		"ErrBadRequest": ErrBadRequest,
		"ErrForbidden":  ErrForbidden,
	}
	for name, err := range all {
		if err == nil {
			t.Errorf("%s is nil", name)
		}
	}
	seen := map[error]string{}
	for name, err := range all {
		if other, dup := seen[err]; dup {
			t.Errorf("%s and %s are the same sentinel", name, other)
		}
		seen[err] = name
	}
}

func TestErrorsIsRoundTrip(t *testing.T) {
	cases := []error{ErrNotFound, ErrConflict, ErrBadRequest, ErrForbidden}
	for _, sentinel := range cases {
		wrapped := fmt.Errorf("context: %w", sentinel)
		if !errors.Is(wrapped, sentinel) {
			t.Errorf("errors.Is failed to match wrapped %v", sentinel)
		}
	}
}
