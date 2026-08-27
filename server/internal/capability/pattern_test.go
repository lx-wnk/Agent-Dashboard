package capability_test

import (
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/capability"
)

func TestMatch(t *testing.T) {
	tests := []struct {
		name    string
		grant   string
		request string
		want    bool
	}{
		{"empty grant is a wildcard", "", "anything at all", true},
		{"exact matches itself", "git status", "git status", true},
		{"exact does not cover a longer command", "git status", "git status --short", false},
		{"prefix covers the longer command", "git status*", "git status --short", true},
		{"prefix matches the bare prefix too", "git status*", "git status", true},
		{"prefix does not cover a different command", "git status*", "git push", false},
		{"domain matches the host", "domain:docs.example.com", "https://docs.example.com/a", true},
		{"domain matches a subdomain", "domain:example.com", "https://docs.example.com/a", true},
		{"domain rejects a suffix collision", "domain:example.com", "https://evilexample.com/a", false},
		{"domain rejects a different host", "domain:example.com", "https://other.test/a", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := capability.Match(tt.grant, tt.request); got != tt.want {
				t.Errorf("Match(%q, %q) = %v, want %v", tt.grant, tt.request, got, tt.want)
			}
		})
	}
}
