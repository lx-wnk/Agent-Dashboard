package askq

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadFixture(t *testing.T, name string) []string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return strings.Split(string(content), "\n")
}

func optionLabels(q *DetectedQuestion) []string {
	labels := make([]string, len(q.Options))
	for i, o := range q.Options {
		labels[i] = o.Label
	}
	return labels
}

func findOption(q *DetectedQuestion, label string) *DetectedOption {
	for i := range q.Options {
		if q.Options[i].Label == label {
			return &q.Options[i]
		}
	}
	return nil
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestDetectQuestionFixtures(t *testing.T) {
	t.Run("single-select", func(t *testing.T) {
		q := DetectQuestion(loadFixture(t, "askq-single.txt"))
		if q == nil {
			t.Fatal("expected non-nil DetectedQuestion")
		}
		if q.MultiSelect {
			t.Error("expected MultiSelect=false")
		}
		if got := optionLabels(q); !equalStrings(got, []string{"Red", "Green", "Blue"}) {
			t.Errorf("labels = %v, want [Red Green Blue]", got)
		}
		if q.TypeSomethingIndex != 4 {
			t.Errorf("TypeSomethingIndex = %d, want 4", q.TypeSomethingIndex)
		}
		if q.ChatAboutIndex != 5 {
			t.Errorf("ChatAboutIndex = %d, want 5", q.ChatAboutIndex)
		}
		red := findOption(q, "Red")
		if red == nil || red.Description != "A warm colour" {
			t.Errorf("Red description = %+v, want 'A warm colour'", red)
		}
		if q.Question != "What is your favourite colour?" {
			t.Errorf("Question = %q, want 'What is your favourite colour?'", q.Question)
		}
	})

	t.Run("multi-select", func(t *testing.T) {
		q := DetectQuestion(loadFixture(t, "askq-multi.txt"))
		if q == nil {
			t.Fatal("expected non-nil DetectedQuestion")
		}
		if !q.MultiSelect {
			t.Error("expected MultiSelect=true")
		}
		if got := optionLabels(q); !equalStrings(got, []string{"Apples", "Bananas", "Cherries"}) {
			t.Errorf("labels = %v, want [Apples Bananas Cherries]", got)
		}
		if q.TypeSomethingIndex != 4 {
			t.Errorf("TypeSomethingIndex = %d, want 4", q.TypeSomethingIndex)
		}
		if q.ChatAboutIndex != 5 {
			t.Errorf("ChatAboutIndex = %d, want 5", q.ChatAboutIndex)
		}
		apples := findOption(q, "Apples")
		if apples == nil || apples.Description != "Crisp and sweet" {
			t.Errorf("Apples description = %+v, want 'Crisp and sweet'", apples)
		}
	})

	t.Run("nonmodal", func(t *testing.T) {
		q := DetectQuestion(loadFixture(t, "askq-nonmodal.txt"))
		if q != nil {
			t.Errorf("expected nil, got %+v", q)
		}
	})

	t.Run("v2.1.205 render with trailing-period meta-row", func(t *testing.T) {
		q := DetectQuestion(loadFixture(t, "askq-v2_1_205.txt"))
		if q == nil {
			t.Fatal("expected non-nil DetectedQuestion")
		}
		if q.MultiSelect {
			t.Error("expected MultiSelect=false")
		}
		if got := optionLabels(q); !equalStrings(got, []string{"Red", "Green", "Blue"}) {
			t.Errorf("labels = %v, want [Red Green Blue]", got)
		}
		if q.TypeSomethingIndex != 4 {
			t.Errorf("TypeSomethingIndex = %d, want 4", q.TypeSomethingIndex)
		}
		if q.ChatAboutIndex != 5 {
			t.Errorf("ChatAboutIndex = %d, want 5", q.ChatAboutIndex)
		}
		red := findOption(q, "Red")
		if red == nil || red.Description != "Warm, high-energy hue." {
			t.Errorf("Red description = %+v, want 'Warm, high-energy hue.'", red)
		}
	})
}

func TestDetectQuestionInline(t *testing.T) {
	t.Run("no meta rows returns nil", func(t *testing.T) {
		q := DetectQuestion([]string{"1. Red", "2. Green", "3. Blue"})
		if q != nil {
			t.Errorf("expected nil, got %+v", q)
		}
	})

	t.Run("no border, leading/trailing whitespace", func(t *testing.T) {
		raw := []string{
			"  Pick a colour  ",
			"",
			"  What is your favourite colour?  ",
			"",
			"  1. Red",
			"  2. Green",
			"  3. Type something",
			"  4. Chat about this",
		}
		q := DetectQuestion(raw)
		if q == nil {
			t.Fatal("expected non-nil DetectedQuestion")
		}
		if got := optionLabels(q); !equalStrings(got, []string{"Red", "Green"}) {
			t.Errorf("labels = %v, want [Red Green]", got)
		}
		if q.MultiSelect {
			t.Error("expected MultiSelect=false")
		}
	})

	t.Run("no modal present", func(t *testing.T) {
		q := DetectQuestion([]string{"just a prompt", "> "})
		if q != nil {
			t.Errorf("expected nil, got %+v", q)
		}
	})

	t.Run("type something without chat about this returns nil", func(t *testing.T) {
		raw := []string{
			"To add an icon:",
			"1. Click icon",
			"2. Type something",
			"3. Press enter",
		}
		q := DetectQuestion(raw)
		if q != nil {
			t.Errorf("expected nil, got %+v", q)
		}
	})

	t.Run("toggle wording in question does not flip multiSelect", func(t *testing.T) {
		raw := []string{
			"Would you like to toggle X?",
			"",
			"1. Yes",
			"   press space to confirm later",
			"2. No",
			"3. Type something",
			"4. Chat about this",
		}
		q := DetectQuestion(raw)
		if q == nil {
			t.Fatal("expected non-nil DetectedQuestion")
		}
		if q.MultiSelect {
			t.Error("expected MultiSelect=false")
		}
	})

	t.Run("index invariant for 1-option modal", func(t *testing.T) {
		raw := []string{
			"Confirm",
			"Proceed?",
			"1. Yes",
			"2. Type something",
			"3. Chat about this",
		}
		q := DetectQuestion(raw)
		if q == nil {
			t.Fatal("expected non-nil DetectedQuestion")
		}
		if len(q.Options) != 1 {
			t.Errorf("len(Options) = %d, want 1", len(q.Options))
		}
		if q.TypeSomethingIndex != len(q.Options)+1 {
			t.Errorf("TypeSomethingIndex = %d, want %d", q.TypeSomethingIndex, len(q.Options)+1)
		}
		if q.ChatAboutIndex != len(q.Options)+2 {
			t.Errorf("ChatAboutIndex = %d, want %d", q.ChatAboutIndex, len(q.Options)+2)
		}
	})

	t.Run("index invariant for 5-option modal", func(t *testing.T) {
		raw := []string{
			"Pick one",
			"Which number?",
			"1. One",
			"2. Two",
			"3. Three",
			"4. Four",
			"5. Five",
			"6. Type something",
			"7. Chat about this",
		}
		q := DetectQuestion(raw)
		if q == nil {
			t.Fatal("expected non-nil DetectedQuestion")
		}
		if len(q.Options) != 5 {
			t.Errorf("len(Options) = %d, want 5", len(q.Options))
		}
		if q.TypeSomethingIndex != 6 {
			t.Errorf("TypeSomethingIndex = %d, want 6", q.TypeSomethingIndex)
		}
		if q.ChatAboutIndex != 7 {
			t.Errorf("ChatAboutIndex = %d, want 7", q.ChatAboutIndex)
		}
	})

	t.Run("rejects frame with numbered description line (index desync)", func(t *testing.T) {
		raw := []string{
			"Pick a colour",
			"What is your favourite colour?",
			"1. Red",
			"   2. items match",
			"2. Green",
			"3. Type something",
			"4. Chat about this",
		}
		q := DetectQuestion(raw)
		if q != nil {
			t.Errorf("expected nil, got %+v", q)
		}
	})

	t.Run("multi-select from checkboxes alone with no footer hint", func(t *testing.T) {
		raw := []string{
			"Pick fruits",
			"Which fruits?",
			"1. [ ] Apples",
			"2. [✔] Bananas",
			"3. Type something",
			"4. Chat about this",
		}
		q := DetectQuestion(raw)
		if q == nil {
			t.Fatal("expected non-nil DetectedQuestion")
		}
		if !q.MultiSelect {
			t.Error("expected MultiSelect=true")
		}
	})

	t.Run("falls back to footer toggle hint when no checkboxes", func(t *testing.T) {
		raw := []string{
			"Pick fruits",
			"Which fruits?",
			"1. Apples",
			"2. Bananas",
			"3. Type something",
			"4. Chat about this",
			"Space to toggle · Enter to confirm",
		}
		q := DetectQuestion(raw)
		if q == nil {
			t.Fatal("expected non-nil DetectedQuestion")
		}
		if !q.MultiSelect {
			t.Error("expected MultiSelect=true")
		}
	})

	t.Run("does not flip multiSelect when only some options carry checkboxes", func(t *testing.T) {
		raw := []string{
			"Pick fruits",
			"Which fruits?",
			"1. [ ] Apples",
			"2. Bananas",
			"3. Type something",
			"4. Chat about this",
		}
		q := DetectQuestion(raw)
		if q == nil {
			t.Fatal("expected non-nil DetectedQuestion")
		}
		if q.MultiSelect {
			t.Error("expected MultiSelect=false")
		}
	})
}
