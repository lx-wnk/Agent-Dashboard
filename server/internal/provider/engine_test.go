package provider

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func codexDescriptor() Descriptor {
	return Descriptor{
		ID: "codex", ExeNames: []string{"codex"}, Source: "jsonl",
		SessionGlob: "*.jsonl",
		Parse: ParseSpec{
			EventFilter: &EventFilter{Path: "payload.type", Equals: "token_count"},
			Tokens: TokenSpec{
				Mode:      TokenCumulative,
				Input:     []string{"payload.info.total_token_usage.input_tokens"},
				Output:    []string{"payload.info.total_token_usage.output_tokens"},
				CacheRead: []string{"payload.info.total_token_usage.cached_input_tokens"},
			},
			Model:    []string{"payload.model"},
			Provider: []string{"payload.model_provider"},
		},
	}
}

func TestEngine_CodexCumulative(t *testing.T) {
	r, err := parseJSONL(codexDescriptor(), filepath.Join("testdata", "codex-session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Session.TokenUsage.InputTokens != 31751 || r.Session.TokenUsage.OutputTokens != 2367 {
		t.Fatalf("tokens wrong: %+v", r.Session.TokenUsage)
	}
	if r.Session.TokenUsage.CacheReadTokens != 14720 {
		t.Fatalf("cacheRead wrong: %d", r.Session.TokenUsage.CacheReadTokens)
	}
	if r.Session.Model != "gpt-5-codex" {
		t.Fatalf("model wrong: %q", r.Session.Model)
	}
	if r.Provider != "openai" {
		t.Fatalf("provider wrong: %q", r.Provider)
	}
}

func geminiDescriptor() Descriptor {
	return Descriptor{
		ID: "gemini", ExeNames: []string{"gemini"}, Source: "jsonl",
		SessionGlob: "*.jsonl",
		Parse: ParseSpec{
			EventFilter: &EventFilter{Path: "type", Equals: "gemini"},
			Tokens: TokenSpec{
				Mode:      TokenPerMessage,
				Input:     []string{"tokens.input"},
				Output:    []string{"tokens.output"},
				CacheRead: []string{"tokens.cached"},
			},
			Model: []string{"model"},
		},
	}
}

func TestEngine_CodexLastActivity(t *testing.T) {
	d := codexDescriptor()
	d.Parse.Timestamp = []string{"timestamp"}
	r, err := parseJSONL(d, filepath.Join("testdata", "codex-session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	// Newest timestamp in fixture: "2026-04-19T11:23:41.622Z" (third line).
	want, _ := time.Parse(time.RFC3339Nano, "2026-04-19T11:23:41.622Z")
	if r.Session.LastActivity.IsZero() {
		t.Fatal("LastActivity must not be zero for a session with timestamps")
	}
	if !r.Session.LastActivity.Equal(want) {
		t.Errorf("LastActivity = %v, want %v", r.Session.LastActivity, want)
	}
}

func TestEngine_GeminiLastActivity(t *testing.T) {
	d := geminiDescriptor()
	d.Parse.Timestamp = []string{"timestamp"}
	r, err := parseJSONL(d, filepath.Join("testdata", "gemini-session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	// Newest timestamp in fixture: "2026-04-19T11:00:20.000Z" (fourth line).
	want, _ := time.Parse(time.RFC3339Nano, "2026-04-19T11:00:20.000Z")
	if !r.Session.LastActivity.Equal(want) {
		t.Errorf("LastActivity = %v, want %v", r.Session.LastActivity, want)
	}
}

func TestEngine_GeminiPerMessageSum(t *testing.T) {
	r, err := parseJSONL(geminiDescriptor(), filepath.Join("testdata", "gemini-session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Session.TokenUsage.InputTokens != 2700 {
		t.Fatalf("want 2700 input, got %d", r.Session.TokenUsage.InputTokens)
	}
	if r.Session.TokenUsage.OutputTokens != 760 {
		t.Fatalf("want 760 output, got %d", r.Session.TokenUsage.OutputTokens)
	}
}

func junieDescriptor() Descriptor {
	return Descriptor{
		ID: "junie", ExeNames: []string{"junie"}, Source: "jsonl",
		SessionGlob: "*.jsonl",
		Parse: ParseSpec{
			EventFilter: &EventFilter{Path: "kind", Equals: "LlmResponseMetadataEvent"},
			Tokens: TokenSpec{
				Mode:        TokenPerMessage,
				Input:       []string{"event.agentEvent.modelUsage[].inputTokens", "event.agentEvent.modelUsage[].input"},
				Output:      []string{"event.agentEvent.modelUsage[].outputTokens", "event.agentEvent.modelUsage[].output"},
				CacheRead:   []string{"event.agentEvent.modelUsage[].cacheInputTokens"},
				CacheCreate: []string{"event.agentEvent.modelUsage[].cacheCreateTokens"},
			},
			Model:    []string{"event.agentEvent.modelUsage[].model"},
			Provider: []string{"event.agentEvent.modelUsage[].provider"},
		},
		Cost: CostSpec{Rule: CostInFile, InFilePath: []string{"event.agentEvent.modelUsage[].cost"}},
	}
}

func TestEngine_JunieTokensAndInFileCost(t *testing.T) {
	r, err := parseJSONL(junieDescriptor(), filepath.Join("testdata", "junie-session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Session.TokenUsage.InputTokens != 2034 {
		t.Fatalf("want 2034 input, got %d", r.Session.TokenUsage.InputTokens)
	}
	if r.Session.Model != "claude-opus-4-6" {
		t.Fatalf("model wrong: %q", r.Session.Model)
	}
	if r.InFileCost < 0.0202 || r.InFileCost > 0.0204 {
		t.Fatalf("want ~0.0203 cost, got %f", r.InFileCost)
	}
}

func TestEngine_EmptyAndMalformedFile(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(empty, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := parseJSONL(codexDescriptor(), empty)
	if err != nil {
		t.Fatalf("empty file must not error, got %v", err)
	}
	if r.Session.TokenUsage.InputTokens != 0 {
		t.Fatalf("empty file must yield zero tokens, got %d", r.Session.TokenUsage.InputTokens)
	}
	bad := filepath.Join(dir, "bad.jsonl")
	if err := os.WriteFile(bad, []byte("not json\n{also not\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := parseJSONL(codexDescriptor(), bad); err != nil {
		t.Fatalf("all-malformed file must skip lines, not error, got %v", err)
	}
}

func piDescriptor() Descriptor {
	return Descriptor{
		ID: "pi", ExeNames: []string{"pi"}, Source: "jsonl",
		SessionGlob: "**/*.jsonl",
		Parse: ParseSpec{
			EventFilter: &EventFilter{Path: "message.role", Equals: "assistant"},
			Tokens: TokenSpec{
				Mode:        TokenPerMessage,
				Input:       []string{"message.usage.input"},
				Output:      []string{"message.usage.output"},
				CacheRead:   []string{"message.usage.cacheRead"},
				CacheCreate: []string{"message.usage.cacheWrite"},
			},
			Model:    []string{"message.model"},
			Provider: []string{"message.provider"},
		},
		Cost: CostSpec{Rule: CostInFile, InFilePath: []string{"message.usage.cost.total"}},
	}
}

func TestEngine_PiPerMessageAndInFileCost(t *testing.T) {
	r, err := parseJSONL(piDescriptor(), filepath.Join("testdata", "pi-session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Session.TokenUsage.InputTokens != 2700 { // 1200 + 1500; user line filtered out
		t.Fatalf("want 2700 input, got %d", r.Session.TokenUsage.InputTokens)
	}
	if r.Session.TokenUsage.OutputTokens != 760 { // 340 + 420
		t.Fatalf("want 760 output, got %d", r.Session.TokenUsage.OutputTokens)
	}
	if r.Session.TokenUsage.CacheReadTokens != 220 || r.Session.TokenUsage.CacheCreationTokens != 110 {
		t.Fatalf("cache wrong: %+v", r.Session.TokenUsage)
	}
	if r.Session.Model != "claude-sonnet-4-5" || r.Provider != "anthropic" {
		t.Fatalf("model/provider wrong: %q / %q", r.Session.Model, r.Provider)
	}
	if r.InFileCost < 0.0272 || r.InFileCost > 0.0274 { // 0.0123 + 0.0150
		t.Fatalf("want ~0.0273 cost, got %f", r.InFileCost)
	}
}
