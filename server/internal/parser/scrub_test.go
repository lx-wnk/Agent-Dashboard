package parser

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScrubSecrets_APIKey(t *testing.T) {
	input := "set api_key=supersecretvalue123 in config"
	out := scrubSecrets(input)
	require.NotContains(t, out, "supersecretvalue123")
	require.Contains(t, out, "[REDACTED]")
}

func TestScrubSecrets_OpenAIKey(t *testing.T) {
	input := "use sk-abcdefghijklmnopqrstuvwxyz012345678 to call the API"
	out := scrubSecrets(input)
	require.NotContains(t, out, "sk-abcdef")
	require.Contains(t, out, "[REDACTED]")
}

func TestScrubSecrets_GitHubPAT(t *testing.T) {
	input := "token: ghp_abcdefghijklmnopqrstuvwxyz12345678901"
	out := scrubSecrets(input)
	require.NotContains(t, out, "ghp_")
	require.Contains(t, out, "[REDACTED]")
}

func TestScrubSecrets_CleanTextUnchanged(t *testing.T) {
	input := "The quick brown fox jumps over the lazy dog."
	out := scrubSecrets(input)
	require.Equal(t, input, out)
}

func TestScrubSecrets_EmptyString(t *testing.T) {
	require.Equal(t, "", scrubSecrets(""))
}

func TestScrubSecrets_MultiplePatterns(t *testing.T) {
	input := "api_key=mykey123\nsk-" + strings.Repeat("x", 32)
	out := scrubSecrets(input)
	require.NotContains(t, out, "mykey123")
	require.NotContains(t, out, strings.Repeat("x", 32))
}

// TestScrubSecretsExportedMatchesInternal pins ScrubSecrets to scrubSecrets:
// an exported wrapper that silently diverges from the implementation it
// fronts is worse than no wrapper.
func TestScrubSecretsExportedMatchesInternal(t *testing.T) {
	inputs := []string{
		"",
		"The quick brown fox jumps over the lazy dog.",
		"set api_key=supersecretvalue123 in config",
		"use sk-abcdefghijklmnopqrstuvwxyz012345678 to call the API",
		"token: ghp_abcdefghijklmnopqrstuvwxyz12345678901",
		"api_key=mykey123\nsk-" + strings.Repeat("x", 32),
	}
	for _, in := range inputs {
		require.Equal(t, scrubSecrets(in), ScrubSecrets(in), "ScrubSecrets diverged from scrubSecrets for input %q", in)
	}
}
