package parser_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
)

// buildSessionFile writes n assistant turns to a temp JSONL file and returns its path.
func buildSessionFile(tb testing.TB, turns int) string {
	tb.Helper()

	type usageBlock struct {
		InputTokens         int `json:"input_tokens"`
		OutputTokens        int `json:"output_tokens"`
		CacheCreationTokens int `json:"cache_creation_input_tokens"`
		CacheReadTokens     int `json:"cache_read_input_tokens"`
	}
	type textBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type msgBody struct {
		Role    string     `json:"role"`
		Model   string     `json:"model"`
		Content []textBlock `json:"content"`
		Usage   usageBlock  `json:"usage"`
	}
	type entry struct {
		Type      string  `json:"type"`
		Timestamp string  `json:"timestamp"`
		Message   msgBody `json:"message"`
	}

	f, err := os.CreateTemp(tb.TempDir(), "bench*.jsonl")
	if err != nil {
		tb.Fatal(err)
	}
	defer f.Close()

	e := entry{
		Type:      "assistant",
		Timestamp: "2025-01-15T10:30:00.000Z",
		Message: msgBody{
			Role:  "assistant",
			Model: "claude-sonnet-4-6",
			Content: []textBlock{
				{Type: "text", Text: strings.Repeat("a", 256)},
			},
			Usage: usageBlock{InputTokens: 1000, OutputTokens: 500},
		},
	}
	line, err := json.Marshal(e)
	if err != nil {
		tb.Fatal(err)
	}
	line = append(line, '\n')

	for i := 0; i < turns; i++ {
		if _, err := f.Write(line); err != nil {
			tb.Fatal(err)
		}
	}
	return f.Name()
}

// BenchmarkParseSessionFile_Small measures parsing a session with ~100 turns (typical active agent).
func BenchmarkParseSessionFile_Small(b *testing.B) {
	path := buildSessionFile(b, 100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := parser.ParseSessionFile(path); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkParseSessionFile_Large measures parsing a session with ~1000 turns (long-running agent).
func BenchmarkParseSessionFile_Large(b *testing.B) {
	path := buildSessionFile(b, 1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := parser.ParseSessionFile(path); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkTailRead_Small measures the raw tail-read I/O cost on a small file.
func BenchmarkTailRead_Small(b *testing.B) {
	path := buildSessionFile(b, 100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := parser.TailRead(path); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkTailRead_Large measures the raw tail-read I/O cost when the file exceeds the 32 KB window.
func BenchmarkTailRead_Large(b *testing.B) {
	path := buildSessionFile(b, 2000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := parser.TailRead(path); err != nil {
			b.Fatal(err)
		}
	}
}
