package cmdscope

import (
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

// A plugin's frontmatter is third-party content, and the menu renders the hint
// styled identically to the dashboard's own usage templates. Everything below
// pins the trust boundary at the parser, so every API consumer inherits it.

// maxArgumentHintRunes bounds what the menu renders, ellipsis included: a
// truncated hint gives back one rune so the ellipsis fits inside the cap
// rather than extending past it.
func TestArgumentHint_LongValueIsCapped(t *testing.T) {
	cfg := t.TempDir()
	writeFile(t, filepath.Join(cfg, "commands", "long.md"),
		"---\ndescription: Long\nargument-hint: "+strings.Repeat("a", 5000)+"\n---\n")

	got := hintOf(Scope{Supported: true, ConfigDir: cfg}.SlashCommands(), "/long")

	require.Equal(t, maxArgumentHintRunes, utf8.RuneCountInString(got))
	require.True(t, strings.HasSuffix(got, "…"), "a clipped hint must say it was clipped, got %q", got)
}

// Whitespace collapses (sanitize.ForDisplayCapped, unlike the old per-file
// stripper, folds runs of whitespace into one space) rather than only
// trimming the ends — correct for a hint rendered on a single menu line.
func TestArgumentHint_WhitespaceCollapsed(t *testing.T) {
	cfg := t.TempDir()
	writeFile(t, filepath.Join(cfg, "commands", "spaced.md"),
		"---\ndescription: Spaced\nargument-hint: \"  [base-branch]   [--force]  \"\n---\n")

	got := hintOf(Scope{Supported: true, ConfigDir: cfg}.SlashCommands(), "/spaced")

	require.Equal(t, "[base-branch] [--force]", got)
}

// Runes, not bytes: a hint of multi-byte characters well under the rune cap
// must survive untouched.
func TestArgumentHint_CapCountsRunesNotBytes(t *testing.T) {
	cfg := t.TempDir()
	hint := strings.Repeat("ä", 100)
	writeFile(t, filepath.Join(cfg, "commands", "wide.md"),
		"---\ndescription: Wide\nargument-hint: \""+hint+"\"\n---\n")

	got := hintOf(Scope{Supported: true, ConfigDir: cfg}.SlashCommands(), "/wide")

	require.Equal(t, hint, got)
}

// Raw ESC/BEL (C0) and CSI/NEL (C1) reached the API byte-identical.
func TestArgumentHint_ControlCharactersStripped(t *testing.T) {
	cfg := t.TempDir()
	writeFile(t, filepath.Join(cfg, "commands", "ctrl.md"),
		"---\ndescription: Control\nargument-hint: \"[env]\x1b[31m\x07\u009b2K\u0085 danger\"\n---\n")

	got := hintOf(Scope{Supported: true, ConfigDir: cfg}.SlashCommands(), "/ctrl")

	require.Equal(t, "[env][31m2K danger", got)
}

// Trojan-Source-style reordering: a bidi override can make the rendered hint
// read differently from the bytes the operator would actually type.
func TestArgumentHint_BidiOverridesStripped(t *testing.T) {
	cfg := t.TempDir()
	writeFile(t, filepath.Join(cfg, "commands", "bidi.md"),
		"---\ndescription: Bidi\nargument-hint: \"[safe\u202e--dangerous\u202c\u2066x\u2069]\"\n---\n")

	got := hintOf(Scope{Supported: true, ConfigDir: cfg}.SlashCommands(), "/bidi")

	require.Equal(t, "[safe--dangerousx]", got)
}

func TestArgumentHint_InvalidUTF8Dropped(t *testing.T) {
	cfg := t.TempDir()
	writeFile(t, filepath.Join(cfg, "commands", "binary.md"),
		"---\ndescription: Binary\nargument-hint: \"[env\xff\xfe]\"\n---\n")

	got := hintOf(Scope{Supported: true, ConfigDir: cfg}.SlashCommands(), "/binary")

	require.Empty(t, got, "a hint that is not valid UTF-8 must be dropped, not shipped")
}
