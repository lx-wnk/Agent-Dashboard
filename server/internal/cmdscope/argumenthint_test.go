package cmdscope

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func fieldOf(cmds []SlashCommand, name string, pick func(SlashCommand) string) string {
	for _, c := range cmds {
		if c.Name == name {
			return pick(c)
		}
	}
	return "<not found>"
}

func hintOf(cmds []SlashCommand, name string) string {
	return fieldOf(cmds, name, func(c SlashCommand) string { return c.ArgumentHint })
}

func descOf(cmds []SlashCommand, name string) string {
	return fieldOf(cmds, name, func(c SlashCommand) string { return c.Description })
}

func TestArgumentHint_FromCommandFile(t *testing.T) {
	cfg := t.TempDir()
	writeFile(t, filepath.Join(cfg, "commands", "deploy.md"),
		"---\ndescription: Deploy app\nargument-hint: \"[env] [--dry-run]\"\n---\nbody")

	got := Scope{Supported: true, ConfigDir: cfg}.SlashCommands()

	require.Equal(t, "[env] [--dry-run]", hintOf(got, "/deploy"))
}

// A skill is typeable as /<name>, so its hint has to survive the skill →
// command conversion. This is the path /branch-review takes.
func TestArgumentHint_SurvivesSkillToCommandConversion(t *testing.T) {
	cfg := t.TempDir()
	writeFile(t, filepath.Join(cfg, "skills", "branch-review", "SKILL.md"),
		"---\nname: branch-review\ndescription: Review a branch\nargument-hint: \"[base-branch] [--apply-fixes]\"\n---\nbody")

	got := Scope{Supported: true, ConfigDir: cfg}.SlashCommands()

	require.Equal(t, "[base-branch] [--apply-fixes]", hintOf(got, "/branch-review"))
}

// The keys may appear in either order, so the parser must not stop at whichever
// it reaches first.
func TestArgumentHint_KeyOrderIndependent(t *testing.T) {
	cfg := t.TempDir()
	writeFile(t, filepath.Join(cfg, "commands", "before.md"),
		"---\nargument-hint: \"[x]\"\ndescription: Hint came first\n---\n")
	writeFile(t, filepath.Join(cfg, "commands", "after.md"),
		"---\ndescription: Hint comes later\nargument-hint: \"[y]\"\n---\n")

	got := Scope{Supported: true, ConfigDir: cfg}.SlashCommands()

	require.Equal(t, "[x]", hintOf(got, "/before"))
	require.Equal(t, "Hint came first", descOf(got, "/before"))
	require.Equal(t, "[y]", hintOf(got, "/after"))
	require.Equal(t, "Hint comes later", descOf(got, "/after"))
}

// Real skill files carry a trailing YAML comment after the quoted value.
func TestArgumentHint_QuotedValueDropsTrailingComment(t *testing.T) {
	cfg := t.TempDir()
	writeFile(t, filepath.Join(cfg, "commands", "commented.md"),
		"---\ndescription: Has a comment\nargument-hint: \"[arg]\" # required when user-invocable\n---\n")

	got := Scope{Supported: true, ConfigDir: cfg}.SlashCommands()

	require.Equal(t, "[arg]", hintOf(got, "/commented"))
}

// An apostrophe inside a double-quoted hint must not end the value early.
func TestArgumentHint_ApostropheInsideQuotedValue(t *testing.T) {
	cfg := t.TempDir()
	writeFile(t, filepath.Join(cfg, "commands", "quoted.md"),
		"---\ndescription: Quoting\nargument-hint: \"[component, e.g. 'OrderService']\"\n---\n")

	got := Scope{Supported: true, ConfigDir: cfg}.SlashCommands()

	require.Equal(t, "[component, e.g. 'OrderService']", hintOf(got, "/quoted"))
}

// YAML escapes a literal apostrophe inside a single-quoted scalar by doubling
// it. Ending the value at the first of the pair truncates a real hint — found
// against an installed skill, not imagined.
func TestArgumentHint_SingleQuotedWithDoubledApostrophes(t *testing.T) {
	cfg := t.TempDir()
	writeFile(t, filepath.Join(cfg, "commands", "gazette.md"),
		"---\ndescription: Gazette\nargument-hint: '--topics ''Topic1: terms'' --customers ''c1.de: Stack'''\n---\n")

	got := Scope{Supported: true, ConfigDir: cfg}.SlashCommands()

	require.Equal(t, "--topics 'Topic1: terms' --customers 'c1.de: Stack'", hintOf(got, "/gazette"))
}

// The double-quoted form escapes with a backslash.
func TestArgumentHint_DoubleQuotedWithEscapedQuote(t *testing.T) {
	cfg := t.TempDir()
	writeFile(t, filepath.Join(cfg, "commands", "escaped.md"),
		"---\ndescription: Escaped\nargument-hint: \"[say \\\"hi\\\"]\"\n---\n")

	got := Scope{Supported: true, ConfigDir: cfg}.SlashCommands()

	require.Equal(t, `[say "hi"]`, hintOf(got, "/escaped"))
}

// An unquoted scalar keeps its inner "#" but loses a trailing comment.
func TestArgumentHint_UnquotedDropsTrailingCommentOnly(t *testing.T) {
	cfg := t.TempDir()
	writeFile(t, filepath.Join(cfg, "commands", "bare.md"),
		"---\ndescription: Bare\nargument-hint: [issue#id] # trailing note\n---\n")

	got := Scope{Supported: true, ConfigDir: cfg}.SlashCommands()

	require.Equal(t, "[issue#id]", hintOf(got, "/bare"))
}

// An explicitly empty hint means "takes no arguments" and must not surface as a
// stray template in the menu.
func TestArgumentHint_EmptyStaysEmpty(t *testing.T) {
	cfg := t.TempDir()
	writeFile(t, filepath.Join(cfg, "commands", "noargs.md"),
		"---\ndescription: No args\nargument-hint: \"\"\n---\n")

	got := Scope{Supported: true, ConfigDir: cfg}.SlashCommands()

	require.Empty(t, hintOf(got, "/noargs"))
}

func TestArgumentHint_AbsentLeavesDescriptionIntact(t *testing.T) {
	cfg := t.TempDir()
	writeFile(t, filepath.Join(cfg, "commands", "plain.md"),
		"---\ndescription: Plain command\n---\n")

	got := Scope{Supported: true, ConfigDir: cfg}.SlashCommands()

	require.Empty(t, hintOf(got, "/plain"))
	require.Equal(t, "Plain command", descOf(got, "/plain"))
}

// Block-scalar descriptions resolve against the following indented line; a hint
// after such a description must still be found.
func TestArgumentHint_AfterBlockScalarDescription(t *testing.T) {
	cfg := t.TempDir()
	writeFile(t, filepath.Join(cfg, "commands", "block.md"),
		"---\ndescription: >-\n  Folded description text\nargument-hint: \"[z]\"\n---\n")

	got := Scope{Supported: true, ConfigDir: cfg}.SlashCommands()

	require.Equal(t, "Folded description text", descOf(got, "/block"))
	require.Equal(t, "[z]", hintOf(got, "/block"))
}
