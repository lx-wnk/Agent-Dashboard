package tasks

import (
	"fmt"
	"strings"
)

func BuildPermissionGrantHandoffNote(tool, pattern string, cycleCount int) string {
	toolStr := tool
	if pattern != "" {
		toolStr = fmt.Sprintf("%s (%s)", tool, pattern)
	}
	ordinal := ""
	if cycleCount >= 2 {
		ordinal = fmt.Sprintf("\n\nThis is permission cycle #%d on this stage_run — your prior request_permission call did not cover everything you actually needed. STOP and forward-scan the entire remaining plan now.", cycleCount)
	}
	return fmt.Sprintf(`[PERMISSION GRANTED] You requested permission for "%s". It has been granted.%s

Before your next tool call, scan ALL remaining work in this stage and request_permission ONCE in a single bulk call with every additional tool/pattern you anticipate needing. Pre-granted entries auto-resolve silently; only genuinely new ones surface as ON HOLD. Do not request piecemeal — every missed tool restarts this stage.

Then resume exactly where you left off.`, toolStr, ordinal)
}

func BuildBulkPermissionGrantHandoffNote(grantedTools []struct{ Tool, Pattern string }, cycleCount int) string {
	var lines []string
	for _, g := range grantedTools {
		if g.Pattern != "" {
			lines = append(lines, fmt.Sprintf("  - %s (%s)", g.Tool, g.Pattern))
		} else {
			lines = append(lines, fmt.Sprintf("  - %s", g.Tool))
		}
	}
	plural := "s"
	if len(grantedTools) == 1 {
		plural = ""
	}
	ordinal := ""
	if cycleCount >= 2 {
		ordinal = fmt.Sprintf("\n\nThis is permission cycle #%d on this stage_run — your prior request_permission call did not cover everything you actually needed. STOP and forward-scan the entire remaining plan now.", cycleCount)
	}
	return fmt.Sprintf(`[PERMISSIONS GRANTED — BULK] You requested %d permission%s and the user granted all of them in a single decision:
%s%s

Before your next tool call, scan ALL remaining work in this stage and request_permission ONCE in a single bulk call with every additional tool/pattern you anticipate needing. Pre-granted entries auto-resolve silently; only genuinely new ones surface as ON HOLD. Do not request piecemeal — every missed tool restarts this stage.

Then resume exactly where you left off.`,
		len(grantedTools), plural, strings.Join(lines, "\n"), ordinal)
}
