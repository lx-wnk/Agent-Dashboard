package history

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildJSONLLine returns a minimal JSONL assistant message entry with the given token counts.
func buildJSONLLine(inputTokens, outputTokens int, model, timestamp string) string {
	return `{"type":"message","timestamp":"` + timestamp + `","message":{"role":"assistant","model":"` + model + `","usage":{"input_tokens":` +
		itoa(inputTokens) + `,"output_tokens":` + itoa(outputTokens) + `,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	// Simple positive-integer formatter sufficient for tests.
	s := []byte{}
	for n > 0 {
		s = append([]byte{byte('0' + n%10)}, s...)
		n /= 10
	}
	return string(s)
}

func TestParseTokensFromRaw_ValidJSONLWithUsage(t *testing.T) {
	ts := "2024-01-15T10:30:00.000Z"
	line := buildJSONLLine(100, 50, "claude-opus-4", ts)

	usage, model, lastActivity, err := parseTokensFromRaw(line)
	require.NoError(t, err)

	assert.Equal(t, 100, usage.InputTokens)
	assert.Equal(t, 50, usage.OutputTokens)
	assert.Equal(t, "claude-opus-4", model)
	// lastActivity should be the parsed timestamp.
	expected, _ := time.Parse(time.RFC3339Nano, ts)
	assert.True(t, lastActivity.Equal(expected) || lastActivity.After(expected),
		"lastActivity should be at or after the parsed timestamp")
}

func TestParseTokensFromRaw_MultipleEntriesAccumulatesTokens(t *testing.T) {
	ts1 := "2024-01-15T10:00:00.000Z"
	ts2 := "2024-01-15T11:00:00.000Z"
	line1 := buildJSONLLine(100, 50, "claude-sonnet-4-5", ts1)
	line2 := buildJSONLLine(200, 80, "claude-sonnet-4-5", ts2)
	raw := line1 + "\n" + line2

	usage, model, lastActivity, err := parseTokensFromRaw(raw)
	require.NoError(t, err)

	assert.Equal(t, 300, usage.InputTokens, "input tokens should be summed")
	assert.Equal(t, 130, usage.OutputTokens, "output tokens should be summed")
	assert.Equal(t, "claude-sonnet-4-5", model)

	// lastActivity should be ts2 (the later one).
	expected, _ := time.Parse(time.RFC3339Nano, ts2)
	assert.True(t, lastActivity.Equal(expected) || lastActivity.After(expected))
}

func TestParseTokensFromRaw_MissingUsageField(t *testing.T) {
	// Assistant message without "usage" field — should not panic and return zero tokens.
	raw := `{"type":"message","timestamp":"2024-01-15T10:00:00.000Z","message":{"role":"assistant","model":"claude-sonnet-4-5"}}`

	usage, _, _, err := parseTokensFromRaw(raw)
	require.NoError(t, err)
	assert.Equal(t, 0, usage.InputTokens)
	assert.Equal(t, 0, usage.OutputTokens)
}

func TestParseTokensFromRaw_NonAssistantRoleSkipped(t *testing.T) {
	// User messages should be ignored.
	raw := `{"type":"message","timestamp":"2024-01-15T10:00:00.000Z","message":{"role":"user","usage":{"input_tokens":999,"output_tokens":999}}}`

	usage, _, _, err := parseTokensFromRaw(raw)
	require.NoError(t, err)
	assert.Equal(t, 0, usage.InputTokens)
	assert.Equal(t, 0, usage.OutputTokens)
}

func TestParseTokensFromRaw_NonMessageTypeSkipped(t *testing.T) {
	// Only entries with type=="message" count.
	raw := `{"type":"tool_result","timestamp":"2024-01-15T10:00:00.000Z","message":{"role":"assistant","usage":{"input_tokens":999,"output_tokens":999}}}`

	usage, _, _, err := parseTokensFromRaw(raw)
	require.NoError(t, err)
	assert.Equal(t, 0, usage.InputTokens)
	assert.Equal(t, 0, usage.OutputTokens)
}

func TestParseTokensFromRaw_MalformedJSONLineSkipped(t *testing.T) {
	// Malformed line followed by a valid line — the bad one is skipped, not an error.
	ts := "2024-01-15T12:00:00.000Z"
	validLine := buildJSONLLine(42, 21, "claude-opus-4", ts)
	raw := "THIS IS NOT JSON\n" + validLine

	usage, _, _, err := parseTokensFromRaw(raw)
	require.NoError(t, err, "malformed JSON should be skipped, not cause an error")
	assert.Equal(t, 42, usage.InputTokens)
	assert.Equal(t, 21, usage.OutputTokens)
}

func TestParseTokensFromRaw_EmptyInput(t *testing.T) {
	usage, model, _, err := parseTokensFromRaw("")
	require.NoError(t, err)
	assert.Equal(t, 0, usage.InputTokens)
	assert.Equal(t, 0, usage.OutputTokens)
	// Default model should be set.
	assert.NotEmpty(t, model)
}

func TestParseTokensFromRaw_AllBlankLines(t *testing.T) {
	raw := strings.Repeat("\n", 10)
	usage, _, _, err := parseTokensFromRaw(raw)
	require.NoError(t, err)
	assert.Equal(t, 0, usage.InputTokens)
	assert.Equal(t, 0, usage.OutputTokens)
}

func TestParseTokensFromRaw_ModelUpdatedToLatestEntry(t *testing.T) {
	ts1 := "2024-01-15T10:00:00.000Z"
	ts2 := "2024-01-15T11:00:00.000Z"
	line1 := buildJSONLLine(10, 5, "claude-sonnet-4-5", ts1)
	line2 := buildJSONLLine(10, 5, "claude-opus-4", ts2)
	raw := line1 + "\n" + line2

	_, model, _, err := parseTokensFromRaw(raw)
	require.NoError(t, err)
	// The last assistant entry's model wins.
	assert.Equal(t, "claude-opus-4", model)
}

func TestParseTokensFromRaw_AllMalformedLines(t *testing.T) {
	raw := "not json\nalso not json\n{broken"
	usage, _, _, err := parseTokensFromRaw(raw)
	require.NoError(t, err)
	assert.Equal(t, 0, usage.InputTokens)
	assert.Equal(t, 0, usage.OutputTokens)
}
