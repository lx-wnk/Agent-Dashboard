package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTranslate_RuleBased(t *testing.T) {
	n := NewNLCron(nil)
	cases := map[string]string{
		"every day at 9am":     "0 9 * * *",
		"daily at 18:00":       "0 18 * * *",
		"every weekday at 9am": "0 9 * * 1-5",
		"every monday at 8:30": "30 8 * * 1",
		"every hour":           "0 * * * *",
		"hourly":               "0 * * * *",
		"every 15 minutes":     "*/15 * * * *",
		"every 60 minutes":     "0 * * * *", // boundary: == hourly
		"every 2 hours":        "0 */2 * * *",
		"every 24 hours":       "0 0 * * *", // boundary: == daily
		"at midnight":          "0 0 * * *",
		"at noon":              "0 12 * * *",
		"weekdays":             "0 0 * * 1-5",
		"every weekend":        "0 0 * * 0,6",
		"every sunday at 11pm": "0 23 * * 0",
		"0 9 * * 1-5":          "0 9 * * 1-5", // raw cron passthrough
	}
	for phrase, want := range cases {
		got, err := n.Translate(context.Background(), phrase)
		if err != nil {
			t.Errorf("Translate(%q) error: %v", phrase, err)
			continue
		}
		if got != want {
			t.Errorf("Translate(%q) = %q, want %q", phrase, got, want)
		}
	}
}

func TestTranslate_Rejection(t *testing.T) {
	n := NewNLCron(nil)
	for _, phrase := range []string{"", "   ", "whenever I feel like it", "purple monkey dishwasher"} {
		if _, err := n.Translate(context.Background(), phrase); !errors.Is(err, ErrUnparseable) {
			t.Errorf("Translate(%q) expected ErrUnparseable, got %v", phrase, err)
		}
	}
}

type stubLLM struct {
	called bool
	resp   string
	err    error
}

func (s *stubLLM) TranslateToCron(_ context.Context, _ string) (string, error) {
	s.called = true
	return s.resp, s.err
}

func TestTranslate_LLMFallback(t *testing.T) {
	// Rule-based misses, LLM returns a valid expression.
	stub := &stubLLM{resp: "30 6 1 * *"}
	n := NewNLCron(stub)
	got, err := n.Translate(context.Background(), "first of the month at 6:30 sharp please")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !stub.called {
		t.Error("LLM must be called when rule-based path declines the phrase")
	}
	if got != "30 6 1 * *" {
		t.Fatalf("got %q, want %q", got, "30 6 1 * *")
	}
}

func TestTranslate_LLMNotCalledForRuleHit(t *testing.T) {
	stub := &stubLLM{}
	n := NewNLCron(stub)
	if _, err := n.Translate(context.Background(), "every hour"); err != nil {
		t.Fatal(err)
	}
	if stub.called {
		t.Error("LLM must not be called when rule-based path succeeds")
	}
}

func TestTranslate_LLMInvalidStillRejects(t *testing.T) {
	n := NewNLCron(&stubLLM{resp: "not a cron"})
	if _, err := n.Translate(context.Background(), "some weird phrase"); !errors.Is(err, ErrUnparseable) {
		t.Fatalf("expected ErrUnparseable, got %v", err)
	}
}

func TestValidate(t *testing.T) {
	if err := Validate("0 9 * * 1-5"); err != nil {
		t.Errorf("valid expr rejected: %v", err)
	}
	if err := Validate("99 99 * * *"); err == nil {
		t.Error("invalid expr accepted")
	}
}

func TestNextRuns(t *testing.T) {
	after := time.Date(2026, 6, 14, 8, 0, 0, 0, time.UTC) // Sunday
	runs, err := NextRuns("0 9 * * 1-5", after, 3)
	if err != nil {
		t.Fatalf("NextRuns error: %v", err)
	}
	if len(runs) != 3 {
		t.Fatalf("want 3 runs, got %d", len(runs))
	}
	// First weekday 09:00 after Sun 2026-06-14 is Mon 2026-06-15 09:00.
	want := time.Date(2026, 6, 15, 9, 0, 0, 0, time.UTC)
	if !runs[0].Equal(want) {
		t.Errorf("first run = %v, want %v", runs[0], want)
	}
}
