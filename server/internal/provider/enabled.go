package provider

// EnabledFunc reports whether a provider id is enabled. Plan 2 supplies a
// DB-backed implementation; the default below uses the descriptor's own flag
// OR-ed with an explicit enabled-id allowlist (from DASHBOARD_PROVIDERS_ENABLED).
type EnabledFunc func(id string) bool

// DefaultEnabled returns an EnabledFunc: a provider is on if its descriptor sets
// enabled:true OR its id appears in allow.
func DefaultEnabled(descriptors map[string]Descriptor, allow []string) EnabledFunc {
	set := map[string]bool{}
	for _, id := range allow {
		set[id] = true
	}
	return func(id string) bool {
		if set[id] {
			return true
		}
		d, ok := descriptors[id]
		return ok && d.Enabled
	}
}
