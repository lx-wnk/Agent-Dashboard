package plugin

// ValidID reports whether id is a well-formed plugin identifier.
// Uses pluginIDRe as the single source of truth for the validation pattern.
func ValidID(id string) bool {
	return pluginIDRe.MatchString(id)
}
