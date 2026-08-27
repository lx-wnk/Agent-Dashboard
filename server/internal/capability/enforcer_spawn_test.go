package capability_test

import (
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/capability"
)

func TestSpawnEnforcerEmitsOnlyAllowedEntries(t *testing.T) {
	e := capability.SpawnEnforcer{}
	decisions := []capability.Decision{
		{Effect: capability.EffectAllow},
		{Effect: capability.EffectDeny},
		{Effect: capability.EffectAsk},
	}
	entries := []capability.AllowEntry{
		{Tool: "Bash", Pattern: "git status*"},
		{Tool: "Bash", Pattern: "curl evil"},
		{Tool: "WebFetch", Pattern: "domain:docs.example.com"},
	}

	got := e.AllowList(decisions, entries)
	want := []string{"Bash(git status*)"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("AllowList = %v, want %v — only an allow becomes a settings entry", got, want)
	}
}

func TestSpawnEnforcerAskIsNotAnAllow(t *testing.T) {
	e := capability.SpawnEnforcer{}
	got := e.AllowList(
		[]capability.Decision{{Effect: capability.EffectAsk}},
		[]capability.AllowEntry{{Tool: "Bash", Pattern: "rm -rf"}},
	)
	if len(got) != 0 {
		t.Errorf("AllowList = %v, want empty — the spawn point cannot ask, so ask means not allowed here", got)
	}
}

func TestSpawnEnforcerPoint(t *testing.T) {
	if got := (capability.SpawnEnforcer{}).Point(); got != capability.EnforcerSpawn {
		t.Errorf("Point() = %q, want %q", got, capability.EnforcerSpawn)
	}
}
