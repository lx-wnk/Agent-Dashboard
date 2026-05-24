package permissions

import "fmt"

// TemplateTools is the canonical mapping from permission template name to tool list.
// Both the MCP write tools and the REST bulk-grant endpoint derive from this map,
// so template drift between the two call sites is impossible.
//
// Security rule (F-SEC-004): WebFetch is intentionally absent from all templates.
// A blanket WebFetch grant (no domain pattern) enables prompt-injection exfiltration.
// Agents that need network access must request an explicit WebFetch grant with a
// domain pattern via the request_permission MCP tool or the bulk-grant REST endpoint.
// Use ValidateWebFetchPattern before accepting any WebFetch permission entry.
var TemplateTools = map[string][]string{
	// feature_implementation: full file + shell access; WebFetch requires explicit domain grant.
	"feature_implementation": {"Read", "Write", "Edit", "MultiEdit", "Glob", "Grep", "LS", "Bash"},
	// concept_baseline: read-only exploration + web search; WebFetch excluded (use explicit domain grant).
	"concept_baseline": {"Read", "Glob", "Grep", "WebSearch"},
	// research_only: read + search; WebFetch excluded — agents must request a domain-scoped grant.
	"research_only": {"Read", "Glob", "Grep", "LS", "WebSearch"},
	// test_only: compile/test cycle; no network access required.
	"test_only": {"Read", "Write", "Edit", "Glob", "Grep", "LS", "Bash"},
	// review_only: passive read access; no write, no network.
	"review_only": {"Read", "Glob", "Grep", "LS"},
}

// ResolveTemplate returns the tool list for a named template.
func ResolveTemplate(name string) ([]string, error) {
	tools, ok := TemplateTools[name]
	if !ok {
		return nil, fmt.Errorf("unknown permission template: %s", name)
	}
	return tools, nil
}
