package provider

import (
	"testing"

	"github.com/lx-wnk/agent-dashboard/sdk"
)

func testRegistry(t *testing.T, enabled ...string) *Registry {
	t.Helper()
	reg, err := NewRegistry(Options{
		UserDir:   "",
		EnabledFn: nil,
		Ollama:    NewOllamaClassifier("http://127.0.0.1:1"),
		Pricing:   stubPricing{},
	})
	if err != nil {
		t.Fatal(err)
	}
	reg.SetEnabled(DefaultEnabled(reg.Descriptors(), enabled))
	return reg
}

type stubPricing struct{}

func (stubPricing) HasPricing(model string) bool { return model == "gpt-5-codex" }
func (stubPricing) EstimateCost(u sdk.TokenUsage, model string) float64 {
	return float64(u.InputTokens) / 1000
}
func (stubPricing) EstimateCacheCreationCost(sdk.TokenUsage, string) float64 { return 0 }
func (stubPricing) EstimateCacheReadCost(sdk.TokenUsage, string) float64     { return 0 }

func TestRegistry_LoadsBuiltins(t *testing.T) {
	reg := testRegistry(t)
	for _, id := range []string{"codex", "gemini", "junie"} {
		if _, ok := reg.Descriptors()[id]; !ok {
			t.Fatalf("missing built-in descriptor %q", id)
		}
	}
}

func TestRegistry_DetectProviderOnlyWhenEnabled(t *testing.T) {
	reg := testRegistry(t)
	if got := reg.DetectProvider("/usr/local/bin/codex"); got != "" {
		t.Fatalf("disabled provider must not detect, got %q", got)
	}
	reg2 := testRegistry(t, "codex")
	if got := reg2.DetectProvider("codex --foo"); got != sdk.Provider("codex") {
		t.Fatalf("enabled codex should detect, got %q", got)
	}
}

func TestRegistry_CostByModelKnown(t *testing.T) {
	reg := testRegistry(t, "codex")
	cb := reg.Cost("codex", sdk.TokenUsage{InputTokens: 2000}, "gpt-5-codex", 0, "openai")
	if cb.Unknown || cb.Local {
		t.Fatalf("known cloud model should be priced, got %+v", cb)
	}
	if cb.Total != 2.0 {
		t.Fatalf("want 2.0, got %f", cb.Total)
	}
}

func TestRegistry_CostLocalWhenProviderOllama(t *testing.T) {
	reg := testRegistry(t, "codex")
	cb := reg.Cost("codex", sdk.TokenUsage{InputTokens: 9999}, "qwen2.5-coder:7b", 0, "ollama")
	if !cb.Local || cb.Total != 0 {
		t.Fatalf("ollama-provider session should be local $0, got %+v", cb)
	}
}

func TestRegistry_CostInFileJunie(t *testing.T) {
	reg := testRegistry(t, "junie")
	cb := reg.Cost("junie", sdk.TokenUsage{InputTokens: 100}, "claude-opus-4-6", 0.0203, "anthropic")
	if cb.Unknown || cb.Local || cb.Total != 0.0203 {
		t.Fatalf("junie in-file cost should pass through, got %+v", cb)
	}
}
