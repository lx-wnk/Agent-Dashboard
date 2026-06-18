// F037 — parser benchmarks.
// Run with: go test -bench=. -benchmem ./server/internal/parser/

package parser_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// BenchmarkFindSessionForProject_CacheHit measures the hot-path cost of
// FindSessionForProject when the in-memory cache is warm. The directory is
// still stat'd on every call; the win the cache delivers is skipping the
// content tail-read, which this benchmark's alloc baseline guards. Run with:
//
//	go test -bench=CacheHit -benchmem -run=xxx ./internal/parser/
func BenchmarkFindSessionForProject_CacheHit(b *testing.B) {
	orig := parser.SessionCacheTTL
	parser.SessionCacheTTL = 10 * time.Minute
	b.Cleanup(func() { parser.SessionCacheTTL = orig })

	configDir := b.TempDir()
	cwd := "/tmp/bench-project-cache-hit"

	// Write a small session file once.
	path := buildBenchSession(b, 10)
	encoded := parser.EncodePath(cwd)
	dir := filepath.Join(configDir, "projects", encoded)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		b.Fatalf("mkdir: %v", err)
	}
	destName := "00000000-0000-0000-0000-000000000001.jsonl"
	data, err := os.ReadFile(path)
	if err != nil {
		b.Fatalf("read bench session: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, destName), data, 0o640); err != nil {
		b.Fatalf("write bench session: %v", err)
	}

	// Warm the cache before timing.
	if _, err := parser.FindSessionForProject(cwd, 0, configDir); err != nil {
		b.Fatalf("warm-up: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = parser.FindSessionForProject(cwd, 0, configDir)
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
