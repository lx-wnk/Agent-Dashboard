package refine

import (
	"regexp"
	"strings"
)

// Phases is the canonical, ordered set of refinement phases. SSOT for the
// backend; the frontend PHASE_LABELS keys must match these strings exactly.
var Phases = []string{"analysis", "spec", "implementation_plan", "approval"}

// phaseDoneRE matches an inline phase-completion marker, e.g. "__phase_done: spec".
var phaseDoneRE = regexp.MustCompile(`__phase_done:\s*(\w+)`)

// IsValidPhase reports whether p is one of the canonical Phases.
func IsValidPhase(p string) bool {
	for _, v := range Phases {
		if v == p {
			return true
		}
	}
	return false
}

// ExtractPhases returns the content with all "__phase_done: …" markers removed
// and the ordered list of VALID phases they declared (unknown phases ignored).
// Cleaning drops the line a stripped marker occupied (so it does not leave a
// blank line in the persisted content) and trims surrounding whitespace.
func ExtractPhases(s string) (cleaned string, phases []string) {
	for _, m := range phaseDoneRE.FindAllStringSubmatch(s, -1) {
		if IsValidPhase(m[1]) {
			phases = append(phases, m[1])
		}
	}
	// Remove marker lines entirely (line was solely the marker or the whole
	// line is the marker). We split the original on newlines so we can drop
	// lines that were purely a marker, rather than leaving empty lines.
	origLines := strings.Split(s, "\n")
	kept := make([]string, 0, len(origLines))
	for _, ln := range origLines {
		stripped := strings.TrimSpace(phaseDoneRE.ReplaceAllString(ln, ""))
		if stripped == "" && phaseDoneRE.MatchString(ln) {
			// The line contained only a marker — drop it entirely.
			continue
		}
		kept = append(kept, phaseDoneRE.ReplaceAllString(ln, ""))
	}
	cleaned = strings.TrimSpace(strings.Join(kept, "\n"))
	return cleaned, phases
}
