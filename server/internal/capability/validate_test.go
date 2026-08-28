package capability

import "testing"

// wantContextKinds and wantModes are written out rather than derived from
// contextRank/modeRank: the accessors under test are themselves derived from
// those maps, so a test that read them could not fail.
var (
	wantContextKinds = []string{"agent_session", "task", "routine", "application", "project", "global"}
	wantModes        = []string{"deny", "allow", "ask"}
)

func TestIsValidContextKind(t *testing.T) {
	for _, kind := range wantContextKinds {
		if !IsValidContextKind(kind) {
			t.Errorf("IsValidContextKind(%q) = false, want true", kind)
		}
	}
	for _, kind := range []string{"", "nope", "Task"} {
		if IsValidContextKind(kind) {
			t.Errorf("IsValidContextKind(%q) = true, want false", kind)
		}
	}
}

func TestIsValidMode(t *testing.T) {
	for _, mode := range wantModes {
		if !IsValidMode(mode) {
			t.Errorf("IsValidMode(%q) = false, want true", mode)
		}
	}
	for _, mode := range []string{"", "alow", "Allow"} {
		if IsValidMode(mode) {
			t.Errorf("IsValidMode(%q) = true, want false", mode)
		}
	}
}

func TestContextKindsOrder(t *testing.T) {
	want := wantContextKinds
	got := ContextKinds()
	if len(got) != len(want) {
		t.Fatalf("ContextKinds() = %v, want %v", got, want)
	}
	for i, k := range want {
		if got[i] != k {
			t.Fatalf("ContextKinds() = %v, want %v", got, want)
		}
	}
}

func TestModesOrder(t *testing.T) {
	want := wantModes
	got := Modes()
	if len(got) != len(want) {
		t.Fatalf("Modes() = %v, want %v", got, want)
	}
	for i, m := range want {
		if got[i] != m {
			t.Fatalf("Modes() = %v, want %v", got, want)
		}
	}
}
