package taskcontrol

// IsAllowAll reports whether the given autonomy level enables the allow-all
// permission gate (auto-approve all requests, permissive spawn allow-list).
//
// Empty-string autonomy intentionally maps to false: rows that pre-date the
// field (migrated without a value) must keep the old gated behaviour so
// existing tasks are not silently escalated. New tasks receive "spec_gated"
// via the schema default.
func IsAllowAll(autonomy string) bool {
	return autonomy == "spec_gated" || autonomy == "full"
}

// ValidAutonomyValues is the canonical set of accepted autonomy strings.
// Callers must reject any value not in this set.
var ValidAutonomyValues = map[string]struct{}{
	"manual":     {},
	"spec_gated": {},
	"full":       {},
}

// PermissiveAllowList returns the --allowedTools slice for allow-all tasks.
// The operator has opted into unrestricted tool access, so the safe-list check
// for Bash patterns is bypassed and blanket Bash is included.
//
// When allowGitPush is false, blanket Bash is still included but git-push
// containment relies on the spawner's --permission-mode default (same
// behaviour as the existing gitPushRE gate in the restrictive path: the
// restriction is honoured at CLI level, not allow-list level).
func PermissiveAllowList(allowGitPush bool) []string {
	tools := []string{
		"Read",
		"Write",
		"Edit",
		"MultiEdit",
		"Glob",
		"Grep",
		"LS",
		"Agent",
		"WebFetch",
		"WebSearch",
		"Task",
		"TodoRead",
		"TodoWrite",
		"NotebookRead",
		"NotebookEdit",
		// Blanket Bash: allow-all explicitly bypasses the safe-list since the
		// operator has opted into unattended operation.
		"Bash",
	}
	_ = allowGitPush // git-push gate is advisory on the allow-all path (see doc comment)
	return tools
}
