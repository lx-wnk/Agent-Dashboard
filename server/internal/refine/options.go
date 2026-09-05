package refine

import (
	"regexp"
	"strings"
)

// optionsBlockRE matches the out-of-band options block emitted by the refinement
// agent. The block spans from __options_start to __options_end, one option per
// line, at most three kept.
//
// Parity: the frontend strips the same block twice — line by line off the live
// stream in src/features/pipeline/composables/useRefinementChat.ts
// (OPTIONS_START_LINE / OPTIONS_END_LINE) and as a whole out of persisted content
// in src/features/pipeline/components/RefinementChat.vue (OPTIONS_BLOCK_RE).
// Three markers, one grammar; keep them in sync by hand.
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

	// Removes the block itself, not the whitespace around it — a blank line above
	// it survives. The prompt puts the block last, so the TrimSpace below is what
	// actually keeps the persisted content clean.
	cleaned = optionsBlockRE.ReplaceAllString(s, "")
	cleaned = strings.TrimSpace(cleaned)
	return cleaned, options
}
