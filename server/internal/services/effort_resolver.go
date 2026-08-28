package services

import "context"

// AdapterConfigEffortKey is the adapter_config key that carries a spawner's
// reasoning effort setting.
const AdapterConfigEffortKey = "effort"

// effortSupportingAdapterTypes lists the adapter_type values whose adapter
// actually reads the effort setting, declared here rather than guessed at call
// sites. Today that is the claude CLI adapter only. "anthropic" is a separate,
// API-unreachable adapter_type (see llmadapter.NewLLMSpawnerFromSpawner) and is
// intentionally not listed.
var effortSupportingAdapterTypes = map[string]struct{}{
	"claude": {},
}

// ValidEffortLevels are the values the claude CLI's --effort flag actually
// accepts, verified against `claude --help` (v2.1.248): "Effort level for
// the current session (low, medium, high, xhigh, max)". This is wider than
// the three-option dropdown in src/utils/models.ts (EFFORT_OPTIONS), which
// is a deliberate UI simplification — a value set directly through the API
// outside that shortlist (e.g. "xhigh") is still a real CLI level and must
// not be treated as unrecognized here.
var ValidEffortLevels = map[string]struct{}{
	"low":    {},
	"medium": {},
	"high":   {},
	"xhigh":  {},
	"max":    {},
}

// IsValidEffortLevel reports whether v is a level the claude CLI recognizes.
// Callers spawning an agent must fail closed on false: omit the --effort
// argument entirely rather than forward a value that would make the CLI
// invocation itself error.
func IsValidEffortLevel(v string) bool {
	_, ok := ValidEffortLevels[v]
	return ok
}

// ResolveEffort resolves the effective spawner for taskID/stage through r and
// reads its adapter_config effort setting. supported reports whether the
// resolved spawner's adapter_type understands effort at all, so an adapter
// that does not gets the setting reported as inapplicable rather than
// dropped — the same treatment a provider without a skill format gets from
// the materializer. An absent effort key is not an error: effort is "" and
// supported still reflects the adapter type.
func ResolveEffort(ctx context.Context, r SpawnerResolver, taskID, stage string) (effort string, source SpawnerSource, supported bool, err error) {
	sp, src, err := r.Resolve(ctx, taskID, stage)
	if err != nil {
		return "", "", false, err
	}
	_, supported = effortSupportingAdapterTypes[sp.AdapterType]
	return sp.AdapterConfig[AdapterConfigEffortKey], src, supported, nil
}
