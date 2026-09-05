package refine

import (
	"regexp"
	"strings"
)

// optionsBlockRE matches the out-of-band options block emitted by the refinement
// agent. The block spans from __options_start to __options_end, one option per
// line, at most three kept.
//
// Parity: the frontend strips the same block in
// src/features/pipeline/composables/useRefinementChat.ts (OPTIONS_START_RE,
// OPTIONS_END_RE). Keep the two in sync.
var optionsBlockRE = regexp.MustCompile(`(?m)^__options_start\n([\s\S]*?)^__options_end\s*$`)

// maxOptions is the upper bound on prepared answers surfaced to the user.
const maxOptions = 3

// ExtractOptions returns the content with the __options_start…__options_end
// block removed and the parsed option strings (at most three, empty lines
// skipped). A malformed or missing block returns nil options and the original
// content unchanged (trimmed). More than three options: keep the first three.
func ExtractOptions(s string) (cleaned string, options []string) {
	m := optionsBlockRE.FindStringSubmatch(s)
	if m == nil {
		return strings.TrimSpace(s), nil
	}

	// Parse the option lines from the captured group.
	for _, line := range strings.Split(m[1], "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		options = append(options, line)
		if len(options) >= maxOptions {
			break
		}
	}
	if len(options) == 0 {
		options = nil
	}

	// Remove the entire block (including surrounding blank lines that would
	// otherwise leave a visible gap in the persisted content).
	cleaned = optionsBlockRE.ReplaceAllString(s, "")
	cleaned = strings.TrimSpace(cleaned)
	return cleaned, options
}
