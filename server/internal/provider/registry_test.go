package provider

import (
	"os"
	"path/filepath"
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

func TestResolveSession_CodexNestedGlobAndJunieParentID(t *testing.T) {
	// --- codex: date-nested sessions/YYYY/MM/DD/rollout-*.jsonl must match via ** ---
	codexRoot := t.TempDir()
	nested := filepath.Join(codexRoot, "sessions", "2026", "04", "19")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	codexLine := `{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":5,"output_tokens":2}}}}` + "\n"
	if err := os.WriteFile(filepath.Join(nested, "rollout-abc.jsonl"), []byte(codexLine), 0o644); err != nil {
		t.Fatal(err)
	}
	cd := codexDescriptor()
	cd.SessionGlob = "sessions/**/rollout-*.jsonl"
	if got := findSessions(codexRoot, cd.SessionGlob); len(got) != 1 {
		t.Fatalf("findSessions should match the nested codex file, got %v", got)
	}

	// --- junie: two sessions, each <id>/events.jsonl, must yield distinct ids ---
	junieRoot := t.TempDir()
	line := `{"kind":"LlmResponseMetadataEvent","event":{"agentEvent":{"modelUsage":[{"model":"m","inputTokens":1,"outputTokens":1,"cost":0.01}]}}}` + "\n"
	for _, id := range []string{"sess-A", "sess-B"} {
		dir := filepath.Join(junieRoot, "sessions", id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files := findSessions(junieRoot, "sessions/*/events.jsonl")
	if len(files) != 2 {
		t.Fatalf("expected 2 junie session files, got %d (%v)", len(files), files)
	}
	jd := junieDescriptor()
	jd.SessionIDFrom = "parentDir"
	ids := map[string]bool{}
	for _, f := range files {
		ids[sessionID(jd, f)] = true
	}
	if !ids["sess-A"] || !ids["sess-B"] || len(ids) != 2 {
		t.Fatalf("junie session ids should be the parent dirs sess-A/sess-B, got %v", ids)
	}
}

func TestResolveSession_EndToEndCodex(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "sessions", "2026", "04", "19")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":42,"output_tokens":7}}}}` + "\n"
	if err := os.WriteFile(filepath.Join(nested, "rollout-xyz.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", root)
	reg := testRegistry(t, "codex")
	session, _, _, err := reg.ResolveSession("codex", "/some/cwd", map[string]bool{})
	if err != nil {
		t.Fatalf("expected to resolve codex session, got %v", err)
	}
	if session.TokenUsage.InputTokens != 42 {
		t.Fatalf("want 42 input tokens, got %d", session.TokenUsage.InputTokens)
	}
	if session.SessionID != "rollout-xyz" {
		t.Fatalf("want session id rollout-xyz, got %q", session.SessionID)
	}
}

func TestRegistry_KnownProviders(t *testing.T) {
	reg := testRegistry(t, "codex")
	infos := reg.KnownProviders()
	var codex *ProviderInfo
	for i := range infos {
		if infos[i].ID == "codex" {
			codex = &infos[i]
		}
	}
	if codex == nil {
		t.Fatal("codex should be a known provider")
	}
	if codex.DisplayName != "Codex CLI" {
		t.Fatalf("want display name Codex CLI, got %q", codex.DisplayName)
	}
	for _, in := range infos {
		if in.ID == "claude" {
			t.Fatal("claude must not appear in KnownProviders")
		}
	}
}
