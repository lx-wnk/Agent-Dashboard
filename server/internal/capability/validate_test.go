package capability

import "testing"

func TestIsValidContextKind(t *testing.T) {
	for kind := range contextRank {
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
	for mode := range modeRank {
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
	want := []string{"agent_session", "task", "routine", "application", "project", "global"}
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
	want := []string{"deny", "allow", "ask"}
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

func TestAccessorsCoverAllRanks(t *testing.T) {
	if len(ContextKinds()) != len(contextRank) {
		t.Errorf("ContextKinds() has %d entries, contextRank has %d — a map entry was added without updating ContextKinds", len(ContextKinds()), len(contextRank))
	}
	if len(Modes()) != len(modeRank) {
		t.Errorf("Modes() has %d entries, modeRank has %d — a map entry was added without updating Modes", len(Modes()), len(modeRank))
	}
}
