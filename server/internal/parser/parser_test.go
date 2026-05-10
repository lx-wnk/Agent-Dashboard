package parser_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
)

func TestTailRead_ReturnsContent(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "session*.jsonl")
	require.NoError(t, err)
	_, err = f.WriteString(`{"type":"message","message":{"role":"assistant","model":"claude-sonnet-4-6","content":[]}}` + "\n")
	require.NoError(t, err)
	f.Close()

	content, err := parser.TailRead(f.Name())
	require.NoError(t, err)
	require.Contains(t, content, "claude-sonnet-4-6")
}

func TestTailRead_MissingFile(t *testing.T) {
	_, err := parser.TailRead(filepath.Join(t.TempDir(), "missing.jsonl"))
	require.Error(t, err)
}

func TestTailRead_LargeFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "large*.jsonl")
	require.NoError(t, err)
	// Write more than 32KB
	line := `{"type":"message","message":{"role":"assistant","model":"claude-sonnet-4-6","content":[]}}` + "\n"
	for i := 0; i < 500; i++ {
		_, err = f.WriteString(line)
		require.NoError(t, err)
	}
	f.Close()

	content, err := parser.TailRead(f.Name())
	require.NoError(t, err)
	// Should return at most tailBytes (32768) bytes
	require.LessOrEqual(t, len(content), 32768)
	require.Contains(t, content, "claude-sonnet-4-6")
}

func TestTailRead_WithAssistantTurn(t *testing.T) {
	type usageBlock struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	}
	type msgBody struct {
		Role    string        `json:"role"`
		Model   string        `json:"model"`
		Content []interface{} `json:"content"`
		Usage   usageBlock    `json:"usage"`
	}
	type entry struct {
		Type    string  `json:"type"`
		Message msgBody `json:"message"`
	}
	e := entry{
		Type: "message",
		Message: msgBody{
			Role:    "assistant",
			Model:   "claude-sonnet-4-6",
			Content: []interface{}{},
			Usage:   usageBlock{InputTokens: 100, OutputTokens: 50},
		},
	}
	b, err := json.Marshal(e)
	require.NoError(t, err)

	f, err := os.CreateTemp(t.TempDir(), "session*.jsonl")
	require.NoError(t, err)
	_, err = f.WriteString(string(b) + "\n")
	require.NoError(t, err)
	f.Close()

	content, err := parser.TailRead(f.Name())
	require.NoError(t, err)
	require.Contains(t, content, `"role":"assistant"`)
	require.Contains(t, content, `"input_tokens":100`)
}
