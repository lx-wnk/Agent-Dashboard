// F037 — parser benchmarks.
// Run with: go test -bench=. -benchmem ./server/internal/parser/

package parser_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
)

// buildBenchSession writes n assistant JSONL lines into a temp file and returns its path.
func buildBenchSession(b *testing.B, n int) string {
	b.Helper()
	const lineTemplate = `{"type":"assistant","timestamp":"2025-01-15T10:30:00.000Z","message":{"role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"tool_use","name":"Bash","input":{"command":"ls"}}],"usage":{"input_tokens":100,"output_tokens":50,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}` + "\n"

	f, err := os.CreateTemp(b.TempDir(), "bench*.jsonl")
	if err != nil {
		b.Fatalf("create temp file: %v", err)
	}
	var sb strings.Builder
	for i := 0; i < n; i++ {
		sb.WriteString(lineTemplate)
	}
	if _, err := f.WriteString(sb.String()); err != nil {
		b.Fatalf("write temp file: %v", err)
	}
	f.Close()
	return f.Name()
}

// BenchmarkParseSessionFile measures ParseSessionFile over small and large files.
func BenchmarkParseSessionFile(b *testing.B) {
	sizes := []int{10, 100, 500}
	for _, n := range sizes {
		path := buildBenchSession(b, n)
		b.Run(fmt.Sprintf("lines=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _ = parser.ParseSessionFile(path)
			}
		})
	}
}

// BenchmarkTailRead measures the tail-read I/O primitive on files of varying sizes.
func BenchmarkTailRead(b *testing.B) {
	sizes := []int{10, 200, 500}
	for _, n := range sizes {
		path := buildBenchSession(b, n)
		b.Run(fmt.Sprintf("lines=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _ = parser.TailRead(path)
			}
		})
	}
}
