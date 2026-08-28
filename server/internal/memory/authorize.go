package memory

import (
	"context"
	"fmt"

	"github.com/lx-wnk/agent-dashboard/server/internal/capability"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// ScopeKinds lists the scope kind values a caller may name. Shared by every
// transport that accepts a scope from outside (MCP tool args, HTTP query
// params) so the accepted set cannot drift between them.
var ScopeKinds = []string{string(repo.ScopeGlobal), string(repo.ScopeProject), string(repo.ScopeApplication)}

// ParseScope builds a repo.Scope from a caller-supplied kind and ref,
// defaulting an empty kind to global. An unrecognised kind, or a missing ref
// for a non-global scope, fails closed rather than silently resolving to some
// other scope's spaces.
func ParseScope(kind, ref string) (repo.Scope, error) {
	if kind == "" {
		kind = string(repo.ScopeGlobal)
	}
	switch repo.ScopeKind(kind) {
	case repo.ScopeGlobal:
		return repo.GlobalScope(), nil
	case repo.ScopeProject:
		if ref == "" {
			return repo.Scope{}, fmt.Errorf(`scopeRef is required when scope is "project"`)
		}
		return repo.ProjectScope(ref), nil
	case repo.ScopeApplication:
		if ref == "" {
			return repo.Scope{}, fmt.Errorf(`scopeRef is required when scope is "application"`)
		}
		return repo.ApplicationScope(ref), nil
	default:
		return repo.Scope{}, fmt.Errorf("scope must be one of %v", ScopeKinds)
	}
}

// Contexts builds the capability-context chain for a request scoped to
// scope: the scope's own context plus global, the level every grant chain
// falls back to. This lets a project- or application-scoped grant win on
// specificity while a global grant still backs up a scope that has none of
// its own — the same union visibleSpaceScopes (retrieve.go) already applies
// to which spaces are visible, mirrored here for which grants apply.
func Contexts(scope repo.Scope) []capability.Context {
	scope = scope.Normalize()
	if scope.IsGlobal() {
		return []capability.Context{{Kind: string(repo.ScopeGlobal)}}
	}
	return []capability.Context{
		{Kind: string(scope.Kind), Ref: scope.Ref},
		{Kind: string(repo.ScopeGlobal)},
	}
}

// Authorize resolves and enforces a capability.Decide request for capName
// against value in scope's context chain. This is the single gate shared by
// every transport that reaches a memory action in-process (MCP tools, the
// HTTP API) — the enforcement point capability.EnforcerServer names. A
// capability catalogue lookup miss resolves to a zero-value CapabilityView
// (empty Class), which capability.Decide's defaultEffect sends to deny — the
// fail-closed behaviour SeedCapabilities exists to make the normal case.
func Authorize(ctx context.Context, capabilities repo.CapabilityRepo, grants repo.GrantRepo, capName, value string, scope repo.Scope) error {
	var capView capability.CapabilityView
	if row, err := capabilities.Get(ctx, capName); err == nil {
		capView = capability.CapabilityView{Name: row.Name, Class: row.Class, EnforceableBy: row.EnforceableBy}
	}

	grantRows, err := grants.ListForCapability(ctx, capName)
	if err != nil {
		return fmt.Errorf("list grants for %s: %w", capName, err)
	}
	grantViews := make([]capability.GrantView, len(grantRows))
	for i, g := range grantRows {
		grantViews[i] = capability.GrantView{
			ID:                 g.ID,
			Capability:         g.CapabilityName,
			ContextKind:        g.ContextKind,
			ContextRef:         g.ContextRef,
			Pattern:            g.Pattern,
			Mode:               g.Mode,
			LimitCount:         g.LimitCount,
			LimitWindowSeconds: g.LimitWindowSeconds,
			ExpiresAt:          g.ExpiresAt,
			RevokedAt:          g.RevokedAt,
		}
	}

	req := capability.Request{Capability: capName, Value: value, Contexts: Contexts(scope)}
	decision := capability.Decide(req, grantViews, capView)
	// GrantView{} / 0: no rate limit is tracked for memory grants today, the
	// same no-limit shape enforcer_server_test.go exercises for every
	// non-rate-limited caller.
	return capability.ServerEnforcer{}.Enforce(ctx, decision, capability.GrantView{}, 0)
}
