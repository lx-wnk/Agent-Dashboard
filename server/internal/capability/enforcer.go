package capability

// Enforcement points a capability's EnforceableBy set can name. Canonical
// here rather than in the repo package: an enforcement point is policy
// vocabulary — it names where a decision is applied — not persistence
// vocabulary, and this package is Decide's home.
const (
	EnforcerServer = "server"
	EnforcerSpawn  = "spawn"
	EnforcerHook   = "hook"
)
