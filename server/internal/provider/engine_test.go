package provider

import (
	"path/filepath"
	"testing"
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
