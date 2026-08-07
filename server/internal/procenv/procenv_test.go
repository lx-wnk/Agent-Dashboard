package procenv

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePSOutputPicksTheRequestedKeyPerPid(t *testing.T) {
	raw := []byte(
		"4597 claude --mcp-config /tmp/x.json PATH=/usr/bin CLAUDE_CONFIG_DIR=/Users/me/.claude-work SHELL=/bin/zsh\n" +
			"9920 claude --mcp-config /tmp/y.json PATH=/usr/bin SHELL=/bin/zsh\n",
	)

	got := parsePSOutput(raw, "CLAUDE_CONFIG_DIR")

	assert.Equal(t, "/Users/me/.claude-work", got[4597])
	// A process that does not set the key is absent, not empty-valued: callers
	// distinguish "unset" (fall back to the default dir) from "set to nothing".
	_, ok := got[9920]
	assert.False(t, ok)
}

func TestParsePSOutputIgnoresMalformedLines(t *testing.T) {
	raw := []byte("not-a-pid CLAUDE_CONFIG_DIR=/x\n\n77\n")

	assert.Empty(t, parsePSOutput(raw, "CLAUDE_CONFIG_DIR"))
}

func TestParsePSOutputMatchesTheKeyExactlyNotAsASuffix(t *testing.T) {
	raw := []byte("1 claude OLD_CLAUDE_CONFIG_DIR=/wrong CLAUDE_CONFIG_DIR=/right\n")

	assert.Equal(t, "/right", parsePSOutput(raw, "CLAUDE_CONFIG_DIR")[1])
}

// Reads this very process, which is the one environment the test can control.
func TestLookupReadsALiveProcess(t *testing.T) {
	const key = "PROCENV_TEST_MARKER"
	require.NoError(t, os.Setenv(key, "/tmp/marker"))
	t.Cleanup(func() { _ = os.Unsetenv(key) })

	pid := os.Getpid()

	// `ps eww` reports the environment the process was started with, so a
	// variable set after start is only visible on Linux's /proc. Assert the
	// call is well-formed on both, and the value only where it can be seen.
	got := Lookup(t.Context(), []int{pid}, key)
	if v, ok := got[pid]; ok {
		assert.Equal(t, "/tmp/marker", v)
	}
	assert.NotPanics(t, func() { Lookup(t.Context(), nil, key) })
	assert.Nil(t, Lookup(t.Context(), []int{pid}, ""))
}
