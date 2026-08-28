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
// the current session (low, medium, high, xhigh, max)". EFFORT_OPTIONS in
// src/utils/models.ts is hand-kept in parity with this set — the Vue client
// cannot import Go. They must stay equal: a shorter list there renders a
// value set through the API as unrecognised even though the CLI accepts it.
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
//
// No production caller yet, deliberately. The spawn path reads the effort off
// the spawner it has already resolved rather than resolving a second time, so
// what this adds over that is the SOURCE — which spawner in the task, project,
// default chain supplied the value. That is what a "resolved effort, and where
// it came from" display needs, and no such surface exists yet.
func ResolveEffort(ctx context.Context, r SpawnerResolver, taskID, stage string) (effort string, source SpawnerSource, supported bool, err error) {
	sp, src, err := r.Resolve(ctx, taskID, stage)
	if err != nil {
		return "", "", false, err
	}
	_, supported = effortSupportingAdapterTypes[sp.AdapterType]
	return sp.AdapterConfig[AdapterConfigEffortKey], src, supported, nil
}
