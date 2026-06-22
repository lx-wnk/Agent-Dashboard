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
