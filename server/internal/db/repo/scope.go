package repo

import "fmt"

// ScopeKind names the layer a scoped row applies to.
type ScopeKind string

const (
	ScopeGlobal      ScopeKind = "global"
	ScopeProject     ScopeKind = "project"
	ScopeApplication ScopeKind = "application"
)

// Scope is where a resource applies. Ref is empty for ScopeGlobal, and that
// emptiness is a sentinel rather than an accident: SQLite treats two NULLs as
// distinct, so a nullable ref column could not carry a unique index that
// prevents duplicate global rows. pipeline_config already relies on this.
type Scope struct {
	Kind ScopeKind
	Ref  string
}

// GlobalScope returns the scope every resource falls back to.
func GlobalScope() Scope { return Scope{Kind: ScopeGlobal, Ref: ""} }

// ProjectScope scopes to one project working directory.
func ProjectScope(ref string) Scope { return Scope{Kind: ScopeProject, Ref: ref}.Normalize() }

// ApplicationScope scopes to one application resource id.
func ApplicationScope(ref string) Scope {
	return Scope{Kind: ScopeApplication, Ref: ref}.Normalize()
}

// Normalize collapses the representations that mean the same thing, so two
// equal scopes are always equal as struct values.
func (s Scope) Normalize() Scope {
	if s.Kind == ScopeGlobal || s.Ref == "" {
		return Scope{Kind: ScopeGlobal, Ref: ""}
	}
	return s
}

// IsGlobal reports whether this scope is the global fallback layer.
func (s Scope) IsGlobal() bool { return s.Normalize().Kind == ScopeGlobal }

// Validate rejects a scope kind the registry does not know.
func (s Scope) Validate() error {
	switch s.Kind {
	case ScopeGlobal, ScopeProject, ScopeApplication:
		return nil
	default:
		return fmt.Errorf("scope: unknown kind %q", s.Kind)
	}
}
