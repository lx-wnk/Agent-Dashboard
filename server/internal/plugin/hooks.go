package plugin

import (
	authpkg "github.com/lx-wnk/agent-dashboard/server/internal/auth"
)

// Hooks allows callers to react to capabilities discovered during Load.
// Each field is optional — nil hooks are silently skipped.
// Add a new field here when a new plugin capability needs server-side wiring.
type Hooks struct {
	// SetAuth is called when an auth_provider plugin passes health-check.
	SetAuth func(authpkg.OAuthProvider)
	// future capabilities: SetLLM, SetStorage, SetNotify, etc.
}
