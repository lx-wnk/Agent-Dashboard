package permissions

import "fmt"

// TemplateTools is the canonical mapping from permission template name to tool list.
// Both the MCP write tools and the REST bulk-grant endpoint derive from this map,
// so template drift between the two call sites is impossible.
var TemplateTools = map[string][]string{
	"feature_implementation": {"Read", "Write", "Edit", "MultiEdit", "Glob", "Grep", "LS", "Bash", "WebFetch"},
	"concept_baseline":       {"Read", "Glob", "Grep", "WebFetch", "WebSearch"},
	"research_only":          {"Read", "Glob", "Grep", "LS", "WebFetch", "WebSearch"},
	"test_only":              {"Read", "Write", "Edit", "Glob", "Grep", "LS", "Bash"},
	"review_only":            {"Read", "Glob", "Grep", "LS"},
}

// ResolveTemplate returns the tool list for a named template.
func ResolveTemplate(name string) ([]string, error) {
	tools, ok := TemplateTools[name]
	if !ok {
		return nil, fmt.Errorf("unknown permission template: %s", name)
	}
	return tools, nil
}
