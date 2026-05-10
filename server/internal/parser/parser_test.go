package parser_test

import (
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
