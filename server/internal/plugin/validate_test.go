package plugin_test

import (
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
)

func TestValidID(t *testing.T) {
	valid := []string{"my-plugin", "abc", "a1", "a-b-c-1", "a0"}
	for _, id := range valid {
		if !plugin.ValidID(id) {
			t.Errorf("expected ValidID(%q) = true", id)
		}
	}
	invalid := []string{"", "My-Plugin", "UPPER", "../traversal", "-bad", "has space", "under_score"}
	for _, id := range invalid {
		if plugin.ValidID(id) {
			t.Errorf("expected ValidID(%q) = false", id)
		}
	}
}
