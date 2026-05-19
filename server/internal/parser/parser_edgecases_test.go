package parser_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
	"github.com/stretchr/testify/require"
)

// writeSessionLines is a helper that writes JSONL lines to a temp file and returns its path.
func writeSessionLines(t *testing.T, lines []string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "session*.jsonl")
	require.NoError(t, err)
	_, err = f.WriteString(strings.Join(lines, "\n") + "\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

// buildAssistantEntry creates a valid JSONL assistant entry string.
func buildAssistantEntry(t *testing.T, model string, inputTokens, outputTokens int, blocks []map[string]any) string {
	t.Helper()
	contentBytes, err := json.Marshal(blocks)
	require.NoError(t, err)

	msg := map[string]any{
		"role":    "assistant",
		"model":   model,
		"content": json.RawMessage(contentBytes),
		"usage": map[string]int{
			"input_tokens":  inputTokens,
			"output_tokens": outputTokens,
		},
	}
	msgBytes, err := json.Marshal(msg)
	require.NoError(t, err)

	entry := map[string]any{
		"type":      "assistant",
		"timestamp": "2025-01-15T10:30:00.000Z",
		"message":   json.RawMessage(msgBytes),
	}
	b, err := json.Marshal(entry)
	require.NoError(t, err)
	return string(b)
}

// TestParseSessionFile_Empty verifies that an empty JSONL file returns a valid
// (but zero-activity) SessionData without an error.
func TestParseSessionFile_Empty(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "empty*.jsonl")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	data, err := parser.ParseSessionFile(f.Name())
	require.NoError(t, err)
	require.NotNil(t, data)
	require.Equal(t, 0, data.ConversationTurns)
	require.Empty(t, data.LastTools)
}

// TestParseSessionFile_MalformedLines verifies that malformed (non-JSON) lines
// are skipped gracefully and valid lines are still parsed.
func TestParseSessionFile_MalformedLines(t *testing.T) {
	validLine := buildAssistantEntry(t, "claude-sonnet-4-6", 100, 50, []map[string]any{
		{"type": "text", "text": "hello"},
	})
	path := writeSessionLines(t, []string{
		"not-valid-json-at-all",
		"{broken:",
		validLine,
		"another-invalid-line",
	})

	data, err := parser.ParseSessionFile(path)
	require.NoError(t, err)
	require.Equal(t, 1, data.ConversationTurns)
	require.Equal(t, "claude-sonnet-4-6", data.Model)
}

// TestParseSessionFile_TokenAccumulation verifies that tokens are accumulated
// correctly across multiple assistant turns.
func TestParseSessionFile_TokenAccumulation(t *testing.T) {
	line1 := buildAssistantEntry(t, "claude-sonnet-4-6", 100, 50, nil)
	line2 := buildAssistantEntry(t, "claude-sonnet-4-6", 200, 100, nil)
	path := writeSessionLines(t, []string{line1, line2})

	data, err := parser.ParseSessionFile(path)
	require.NoError(t, err)
	require.Equal(t, 2, data.ConversationTurns)
	require.Equal(t, 300, data.TokenUsage.InputTokens)
	require.Equal(t, 150, data.TokenUsage.OutputTokens)
}

// TestParseSessionFile_ConvergenceDetection verifies that 5 identical consecutive
// tool calls trigger the convergence alert.
func TestParseSessionFile_ConvergenceDetection(t *testing.T) {
	lines := make([]string, 5)
	for i := range lines {
		lines[i] = buildAssistantEntry(t, "claude-sonnet-4-6", 10, 5, []map[string]any{
			{"type": "tool_use", "name": "Bash", "input": json.RawMessage(`{"command":"ls"}`)},
		})
	}
	path := writeSessionLines(t, lines)

	data, err := parser.ParseSessionFile(path)
	require.NoError(t, err)
	require.True(t, data.ConvergenceAlert)
	require.Equal(t, "Bash", data.ConvergenceToolName)
}

// TestParseSessionFile_NoConvergenceWithMixedTools verifies that mixed tool calls
// do NOT trigger the convergence alert.
func TestParseSessionFile_NoConvergenceWithMixedTools(t *testing.T) {
	toolNames := []string{"Bash", "Read", "Write", "Bash", "Read"}
	lines := make([]string, len(toolNames))
	for i, name := range toolNames {
		lines[i] = buildAssistantEntry(t, "claude-sonnet-4-6", 10, 5, []map[string]any{
			{"type": "tool_use", "name": name, "input": json.RawMessage(`{}`)},
		})
	}
	path := writeSessionLines(t, lines)

	data, err := parser.ParseSessionFile(path)
	require.NoError(t, err)
	require.False(t, data.ConvergenceAlert)
}

// TestParseSessionFile_ErrorStateQuota verifies that quota-exceeded text sets the error state.
func TestParseSessionFile_ErrorStateQuota(t *testing.T) {
	line := buildAssistantEntry(t, "claude-sonnet-4-6", 10, 5, []map[string]any{
		{"type": "text", "text": "Error: quota exceeded for this month"},
	})
	path := writeSessionLines(t, []string{line})

	data, err := parser.ParseSessionFile(path)
	require.NoError(t, err)
	require.Equal(t, "quota_exhausted", data.ErrorState)
}

// TestParseSessionFile_ErrorStateRateLimit verifies that rate-limit text sets the error state.
func TestParseSessionFile_ErrorStateRateLimit(t *testing.T) {
	line := buildAssistantEntry(t, "claude-sonnet-4-6", 10, 5, []map[string]any{
		{"type": "text", "text": "429 Too Many Requests — rate limit reached"},
	})
	path := writeSessionLines(t, []string{line})

	data, err := parser.ParseSessionFile(path)
	require.NoError(t, err)
	require.Equal(t, "rate_limited", data.ErrorState)
}

// TestParseSessionFile_ErrorStateAuth verifies that auth-failure text sets the error state.
func TestParseSessionFile_ErrorStateAuth(t *testing.T) {
	line := buildAssistantEntry(t, "claude-sonnet-4-6", 10, 5, []map[string]any{
		{"type": "text", "text": "401 Unauthorized — invalid api key provided"},
	})
	path := writeSessionLines(t, []string{line})

	data, err := parser.ParseSessionFile(path)
	require.NoError(t, err)
	require.Equal(t, "auth_failed", data.ErrorState)
}

// TestParseSessionFile_NoErrorStateOnCleanText verifies that clean output leaves ErrorState empty.
func TestParseSessionFile_NoErrorStateOnCleanText(t *testing.T) {
	line := buildAssistantEntry(t, "claude-sonnet-4-6", 10, 5, []map[string]any{
		{"type": "text", "text": "All tests passed successfully!"},
	})
	path := writeSessionLines(t, []string{line})

	data, err := parser.ParseSessionFile(path)
	require.NoError(t, err)
	require.Empty(t, data.ErrorState)
}

// TestParseSessionFile_LastToolsCapped verifies that LastTools returns at most 5 entries.
func TestParseSessionFile_LastToolsCapped(t *testing.T) {
	toolNames := []string{"Read", "Write", "Bash", "Glob", "Grep", "Edit", "MultiEdit"}
	lines := make([]string, len(toolNames))
	for i, name := range toolNames {
		lines[i] = buildAssistantEntry(t, "claude-sonnet-4-6", 10, 5, []map[string]any{
			{"type": "tool_use", "name": name, "input": json.RawMessage(`{}`)},
		})
	}
	path := writeSessionLines(t, lines)

	data, err := parser.ParseSessionFile(path)
	require.NoError(t, err)
	require.Len(t, data.LastTools, 5)
	// LastTools must contain the last 5 tools in order
	require.Equal(t, []string{"Bash", "Glob", "Grep", "Edit", "MultiEdit"}, data.LastTools)
}

// TestParseSessionFile_ToolCounts verifies that tool usage counts are tracked correctly.
func TestParseSessionFile_ToolCounts(t *testing.T) {
	lines := []string{
		buildAssistantEntry(t, "claude-sonnet-4-6", 10, 5, []map[string]any{
			{"type": "tool_use", "name": "Bash", "input": json.RawMessage(`{}`)},
		}),
		buildAssistantEntry(t, "claude-sonnet-4-6", 10, 5, []map[string]any{
			{"type": "tool_use", "name": "Bash", "input": json.RawMessage(`{}`)},
		}),
		buildAssistantEntry(t, "claude-sonnet-4-6", 10, 5, []map[string]any{
			{"type": "tool_use", "name": "Read", "input": json.RawMessage(`{}`)},
		}),
	}
	path := writeSessionLines(t, lines)

	data, err := parser.ParseSessionFile(path)
	require.NoError(t, err)
	require.Equal(t, 2, data.ToolCounts["Bash"])
	require.Equal(t, 1, data.ToolCounts["Read"])
}

// TestParseSessionFile_ModelTracked verifies that the model field from the last
// non-empty assistant message is used.
func TestParseSessionFile_ModelTracked(t *testing.T) {
	lines := []string{
		buildAssistantEntry(t, "claude-haiku-4-5", 10, 5, nil),
		buildAssistantEntry(t, "claude-sonnet-4-6", 20, 10, nil),
	}
	path := writeSessionLines(t, lines)

	data, err := parser.ParseSessionFile(path)
	require.NoError(t, err)
	require.Equal(t, "claude-sonnet-4-6", data.Model)
}

// TestParseSessionFile_NonExistentFile verifies that a missing file returns an error.
func TestParseSessionFile_NonExistentFile(t *testing.T) {
	_, err := parser.ParseSessionFile(filepath.Join(t.TempDir(), "missing.jsonl"))
	require.Error(t, err)
}

// TestParseSessionFile_BlankLines verifies that blank lines in the JSONL are skipped.
func TestParseSessionFile_BlankLines(t *testing.T) {
	validLine := buildAssistantEntry(t, "claude-sonnet-4-6", 50, 25, nil)
	path := writeSessionLines(t, []string{"", "   ", validLine, "", "   "})

	data, err := parser.ParseSessionFile(path)
	require.NoError(t, err)
	require.Equal(t, 1, data.ConversationTurns)
}

// TestParseSessionFile_LargeFile verifies that very large JSONL files are handled
// by the tail-read mechanism without panicking or erroring.
func TestParseSessionFile_LargeFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "large*.jsonl")
	require.NoError(t, err)
	// Write ~200KB of assistant lines (each line ~200 bytes).
	line := buildAssistantEntry(t, "claude-sonnet-4-6", 10, 5, []map[string]any{
		{"type": "text", "text": fmt.Sprintf("output line %d", 0)},
	})
	var sb strings.Builder
	for i := 0; i < 1000; i++ {
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	_, err = f.WriteString(sb.String())
	require.NoError(t, err)
	require.NoError(t, f.Close())

	data, err := parser.ParseSessionFile(f.Name())
	require.NoError(t, err)
	require.NotNil(t, data)
	// Should have parsed at least some turns from the tail window.
	require.Positive(t, data.ConversationTurns)
}

// TestAllClaudeConfigDirs_DashboardEnvOverride verifies that DASHBOARD_CLAUDE_CONFIG_DIRS
// is listed first when set, with no duplicate entries.
func TestAllClaudeConfigDirs_DashboardEnvOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	t.Setenv("DASHBOARD_CLAUDE_CONFIG_DIRS", dir)
	t.Setenv("CLAUDE_CONFIG_DIR", "")

	dirs := parser.AllClaudeConfigDirs()
	require.NotEmpty(t, dirs)
	require.Equal(t, dir, dirs[0], "explicit DASHBOARD_CLAUDE_CONFIG_DIRS must come first")

	// No duplicates.
	seen := make(map[string]int)
	for _, d := range dirs {
		seen[d]++
	}
	for d, count := range seen {
		require.Equal(t, 1, count, "duplicate dir found: %s", d)
	}
}
