package refine

import (
	"reflect"
	"testing"
)

func TestExtractPhases_SingleMarkerStrippedAndCaptured(t *testing.T) {
	cleaned, phases := ExtractPhases("analysis text\n__phase_done: spec\nmore text")
	if want := "analysis text\nmore text"; cleaned != want {
		t.Errorf("cleaned: got %q, want %q", cleaned, want)
	}
	if !reflect.DeepEqual(phases, []string{"spec"}) {
		t.Errorf("phases: got %v, want [spec]", phases)
	}
}

func TestExtractPhases_MultipleMarkersOrderedAndAllStripped(t *testing.T) {
	cleaned, phases := ExtractPhases("__phase_done: analysis\nbody\n__phase_done: spec\n")
	if !reflect.DeepEqual(phases, []string{"analysis", "spec"}) {
		t.Errorf("phases: got %v, want [analysis spec]", phases)
	}
	if want := "body"; cleaned != want {
		t.Errorf("cleaned: got %q, want %q", cleaned, want)
	}
}

func TestExtractPhases_UnknownPhaseIgnored(t *testing.T) {
	cleaned, phases := ExtractPhases("__phase_done: bogus\nkept")
	if len(phases) != 0 {
		t.Errorf("phases: got %v, want empty", phases)
	}
	if want := "kept"; cleaned != want {
		t.Errorf("cleaned: got %q, want %q", cleaned, want)
	}
}

func TestExtractPhases_NoMarker(t *testing.T) {
	cleaned, phases := ExtractPhases("just prose")
	if len(phases) != 0 || cleaned != "just prose" {
		t.Errorf("got (%q, %v), want (\"just prose\", [])", cleaned, phases)
	}
}
