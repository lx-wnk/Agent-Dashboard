package capability

import "fmt"

// AllowEntry is one tool-and-pattern pair a decision was made about.
type AllowEntry struct {
	Tool    string
	Pattern string
}

// SpawnEnforcer turns decisions into the allow list written into a spawned
// agent's settings file.
//
// It is the one enforcement point that cannot ask: the file is written before
// the process starts, so EffectAsk resolves to omission and the agent falls
// back to its own permission prompt.
type SpawnEnforcer struct{}

// Point identifies this enforcement point.
func (SpawnEnforcer) Point() string { return EnforcerSpawn }

// AllowList renders the allowed entries. decisions and entries are parallel
// slices; a length mismatch is a programming error and panics rather than
// silently dropping permissions.
func (SpawnEnforcer) AllowList(decisions []Decision, entries []AllowEntry) []string {
	if len(decisions) != len(entries) {
		panic(fmt.Sprintf("capability: AllowList got %d decisions for %d entries", len(decisions), len(entries)))
	}
	out := make([]string, 0, len(entries))
	for i, d := range decisions {
		if d.Effect != EffectAllow {
			continue
		}
		e := entries[i]
		if e.Pattern == "" {
			out = append(out, e.Tool)
			continue
		}
		out = append(out, fmt.Sprintf("%s(%s)", e.Tool, e.Pattern))
	}
	return out
}
