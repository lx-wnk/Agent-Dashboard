package refine

import (
	"reflect"
	"testing"
)

func TestExtractOptions_ThreeOptions(t *testing.T) {
	cleaned, options := ExtractOptions("Some analysis text.\n__options_start\nOption A\nOption B\nOption C\n__options_end\nTrailing text.")
	if want := "Some analysis text.\n\nTrailing text."; cleaned != want {
		t.Errorf("cleaned: got %q, want %q", cleaned, want)
	}
	if want := []string{"Option A", "Option B", "Option C"}; !reflect.DeepEqual(options, want) {
		t.Errorf("options: got %v, want %v", options, want)
	}
}

func TestExtractOptions_MoreThanThreeKeepsFirstThree(t *testing.T) {
	_, options := ExtractOptions("__options_start\nOne\nTwo\nThree\nFour\n__options_end")
	if want := []string{"One", "Two", "Three"}; !reflect.DeepEqual(options, want) {
		t.Errorf("options: got %v, want %v", options, want)
	}
}

func TestExtractOptions_NoBlock(t *testing.T) {
	cleaned, options := ExtractOptions("  plain content with no markers  \n")
	if want := "plain content with no markers"; cleaned != want {
		t.Errorf("cleaned: got %q, want %q", cleaned, want)
	}
	if options != nil {
		t.Errorf("options: got %v, want nil", options)
	}
}

func TestExtractOptions_MalformedBlockMissingEnd(t *testing.T) {
	s := "before\n__options_start\nOption A\nOption B"
	cleaned, options := ExtractOptions(s)
	if cleaned != s {
		t.Errorf("cleaned: got %q, want %q", cleaned, s)
	}
	if options != nil {
		t.Errorf("options: got %v, want nil", options)
	}
}

func TestExtractOptions_EmptyBlock(t *testing.T) {
	_, options := ExtractOptions("before\n__options_start\n\n__options_end\nafter")
	if options != nil {
		t.Errorf("options: got %v, want nil", options)
	}
}

func TestExtractOptions_BlankLinesBetweenOptionsSkipped(t *testing.T) {
	_, options := ExtractOptions("__options_start\nOption A\n\nOption B\n\n\nOption C\n__options_end\n")
	if want := []string{"Option A", "Option B", "Option C"}; !reflect.DeepEqual(options, want) {
		t.Errorf("options: got %v, want %v", options, want)
	}
}
