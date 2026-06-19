package parser

import (
	"reflect"
	"testing"

	"github.com/lx-wnk/agent-dashboard/sdk"
)

// funcPtr returns the code pointer of a resolver for identity comparison.
func funcPtr(f ProviderSessionResolver) uintptr {
	return reflect.ValueOf(f).Pointer()
}

func TestResolverFor_Dispatch(t *testing.T) {
	cases := []struct {
		name     string
		provider sdk.Provider
		want     ProviderSessionResolver
	}{
		{"codex uses codex stub", sdk.ProviderCodex, ParseCodexSession},
		{"gemini uses codex stub", sdk.ProviderGemini, ParseCodexSession},
		{"claude uses full parser", sdk.ProviderClaude, ParseSessionFile},
		{"empty defaults to full parser", sdk.Provider(""), ParseSessionFile},
		{"unknown defaults to full parser", sdk.Provider("grok"), ParseSessionFile},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := funcPtr(resolverFor(tc.provider)); got != funcPtr(tc.want) {
				t.Errorf("resolverFor(%q) = unexpected resolver", tc.provider)
			}
		})
	}
}

// TestResolveNonClaudeSession_MissingDir verifies a provider with no session
// files returns an error (not a panic) so callers skip the process.
func TestResolveNonClaudeSession_MissingDir(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir()) // exists, but no projects/ entries
	if _, err := ResolveNonClaudeSession(sdk.ProviderCodex, "/no/such/project", nil); err == nil {
		t.Fatal("expected error when no session files exist")
	}
}
