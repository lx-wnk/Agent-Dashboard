package scanner

import (
	"testing"

	"github.com/lx-wnk/agent-dashboard/sdk"
)

type fakeDetector struct{}

func (fakeDetector) DetectProvider(comm string) sdk.Provider {
	if comm == "codex" {
		return sdk.ProviderCodex
	}
	return ""
}

func TestDetectVia_UsesInjectedDetector(t *testing.T) {
	got := detectProviderVia(fakeDetector{}, "codex")
	if got != sdk.ProviderCodex {
		t.Fatalf("want codex, got %q", got)
	}
	if detectProviderVia(fakeDetector{}, "claude") != sdk.ProviderClaude {
		t.Fatal("claude must always resolve")
	}
}

func TestDetectVia_NilDetector(t *testing.T) {
	if detectProviderVia(nil, "claude --x") != sdk.ProviderClaude {
		t.Fatal("nil detector must still resolve claude")
	}
	if detectProviderVia(nil, "codex") != "" {
		t.Fatal("nil detector must not resolve non-claude")
	}
}
