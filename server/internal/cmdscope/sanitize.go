package cmdscope

import (
	"strings"
	"unicode/utf8"
)

// maxArgumentHintRunes caps the argument template a command or skill file may
// contribute. Real hints are a handful of bracketed tokens; the cap exists
// because the file may come from an installed plugin, and the menu renders the
// hint in the same style as the dashboard's own usage templates.
const maxArgumentHintRunes = 120

// sanitizeArgumentHint normalises a frontmatter-supplied argument hint for
// display. Command and skill files are read from the plugin cache as well as
// from user and project directories, so this is where third-party content
// crosses into the API: a value that is not valid UTF-8 is dropped entirely,
// C0/C1 control characters and Unicode bidi overrides are removed, and the
// result is clipped to maxArgumentHintRunes with a trailing ellipsis.
func sanitizeArgumentHint(v string) string {
	if v == "" {
		return ""
	}
	if !utf8.ValidString(v) {
		return ""
	}

	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f:
			return -1
		// Bidi embedding/override and isolate controls (Trojan Source).
		case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
			return -1
		}
		return r
	}, v)
	cleaned = strings.TrimSpace(cleaned)

	if utf8.RuneCountInString(cleaned) > maxArgumentHintRunes {
		cleaned = string([]rune(cleaned)[:maxArgumentHintRunes-1]) + "…"
	}
	return cleaned
}
