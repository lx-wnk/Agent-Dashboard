package parser

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// buildAssistantLine constructs a minimal JSONL line with an assistant role.
func buildAssistantLine(t *testing.T, blocks []map[string]any) string {
	t.Helper()
	content, err := json.Marshal(blocks)
	if err != nil {
		t.Fatalf("marshal blocks: %v", err)
	}
	msg := map[string]any{
		"role":    "assistant",
		"content": json.RawMessage(content),
	}
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal msg: %v", err)
	}
	entry := map[string]any{
		"type":      "assistant",
		"timestamp": "2025-01-15T10:30:00.000Z",
		"message":   json.RawMessage(msgBytes),
	}
	b, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal entry: %v", err)
	}
	return string(b)
}

// TestParseOutputMessages_SimpleAssistantMessage verifies that a plain text
// assistant block is parsed into a single "assistant" OutputMessage.
func TestParseOutputMessages_SimpleAssistantMessage(t *testing.T) {
	line := buildAssistantLine(t, []map[string]any{
		{"type": "text", "text": "Hello, world!"},
	})

	msgs := parseOutputMessages(line, false)

	require.Len(t, msgs, 1)
	require.Equal(t, "assistant", msgs[0].Role)
	require.Equal(t, "Hello, world!", msgs[0].Content)
}

// TestParseOutputMessages_ToolUseBlock verifies that a tool_use block is parsed
// into a "tool_call" OutputMessage with the correct tool name.
func TestParseOutputMessages_ToolUseBlock(t *testing.T) {
	inputJSON, _ := json.Marshal(map[string]any{"file_path": "/foo/bar.go"})
	line := buildAssistantLine(t, []map[string]any{
		{
			"type":  "tool_use",
			"id":    "tu_abc123",
			"name":  "Read",
			"input": json.RawMessage(inputJSON),
		},
	})

	msgs := parseOutputMessages(line, false)

	require.Len(t, msgs, 1)
	require.Equal(t, "tool_call", msgs[0].Role)
	require.Equal(t, "Read", msgs[0].Content)
	require.NotNil(t, msgs[0].ToolName)
	require.Equal(t, "Read", *msgs[0].ToolName)
	require.NotNil(t, msgs[0].FilePath)
	require.Equal(t, "/foo/bar.go", *msgs[0].FilePath)
}

// TestParseOutputMessages_EmptyInput verifies that parseOutputMessages does not
// panic and returns an empty (non-nil) slice for empty input.
func TestParseOutputMessages_EmptyInput(t *testing.T) {
	require.NotPanics(t, func() {
		msgs := parseOutputMessages("", false)
		require.Empty(t, msgs)
	})
}

// TestParseOutputMessages_MultipleBlocks verifies that multiple content blocks
// in a single assistant message are all returned.
func TestParseOutputMessages_MultipleBlocks(t *testing.T) {
	inputJSON, _ := json.Marshal(map[string]any{"path": "/tmp/x"})
	line := buildAssistantLine(t, []map[string]any{
		{"type": "text", "text": "I will read the file."},
		{
			"type":  "tool_use",
			"id":    "tu_xyz",
			"name":  "Read",
			"input": json.RawMessage(inputJSON),
		},
		{"type": "text", "text": "Done."},
	})

	msgs := parseOutputMessages(line, false)

	require.Len(t, msgs, 3)
	require.Equal(t, "assistant", msgs[0].Role)
	require.Equal(t, "tool_call", msgs[1].Role)
	require.Equal(t, "assistant", msgs[2].Role)
}

// TestParseOutputMessages_LastOnly verifies that when lastOnly==true, only the
// final assistant message is returned.
func TestParseOutputMessages_LastOnly(t *testing.T) {
	line1 := buildAssistantLine(t, []map[string]any{
		{"type": "text", "text": "first"},
	})
	line2 := buildAssistantLine(t, []map[string]any{
		{"type": "text", "text": "second"},
	})
	raw := line1 + "\n" + line2

	msgs := parseOutputMessages(raw, true)

	require.Len(t, msgs, 1)
	require.Equal(t, "second", msgs[0].Content)
}
