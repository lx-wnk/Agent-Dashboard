package provider

import "testing"

func TestValidate_RejectsMissingID(t *testing.T) {
	d := Descriptor{ExeNames: []string{"codex"}, Source: "jsonl"}
	if err := d.Validate(); err == nil {
		t.Fatal("expected error for missing id, got nil")
	}
}

func TestValidate_RejectsUnknownSource(t *testing.T) {
	d := Descriptor{ID: "x", ExeNames: []string{"x"}, Source: "magic"}
	if err := d.Validate(); err == nil {
		t.Fatal("expected error for unknown source, got nil")
	}
}

func TestValidate_RejectsUnknownTokenMode(t *testing.T) {
	d := Descriptor{
		ID: "x", ExeNames: []string{"x"}, Source: "jsonl",
		SessionGlob: "*.jsonl",
		Parse:       ParseSpec{Tokens: TokenSpec{Mode: "weird"}},
	}
	if err := d.Validate(); err == nil {
		t.Fatal("expected error for unknown token mode, got nil")
	}
}

func TestValidate_AcceptsMinimalJSONL(t *testing.T) {
	d := Descriptor{
		ID: "codex", ExeNames: []string{"codex"}, Source: "jsonl",
		SessionGlob: "sessions/**/rollout-*.jsonl",
		Parse:       ParseSpec{Tokens: TokenSpec{Mode: TokenCumulative}},
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("expected valid descriptor, got %v", err)
	}
}

func TestValidate_AcceptsCustomSource(t *testing.T) {
	d := Descriptor{ID: "cursor", ExeNames: []string{"cursor"}, Source: "custom:cursor"}
	if err := d.Validate(); err != nil {
		t.Fatalf("expected custom source valid, got %v", err)
	}
}
