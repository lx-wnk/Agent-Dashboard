package parser_test

import (
	"os"
	"strings"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
)

// sampleJSONL contains a realistic multi-turn session log with tool use.
const sampleJSONL = `{"type":"assistant","timestamp":"2025-01-15T10:00:00.000Z","message":{"role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"I will help you with that."}],"usage":{"input_tokens":500,"output_tokens":50,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}
{"type":"assistant","timestamp":"2025-01-15T10:01:00.000Z","message":{"role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"tool_use","name":"Read","input":{"file_path":"/tmp/foo.go"}}],"usage":{"input_tokens":600,"output_tokens":30,"cache_creation_input_tokens":200,"cache_read_input_tokens":0}}}
{"type":"assistant","timestamp":"2025-01-15T10:02:00.000Z","message":{"role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/tmp/foo.go","old_string":"a","new_string":"b"}}],"usage":{"input_tokens":700,"output_tokens":40,"cache_creation_input_tokens":0,"cache_read_input_tokens":300}}}
{"type":"assistant","timestamp":"2025-01-15T10:03:00.000Z","message":{"role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}},{"type":"text","text":"Running tests..."}],"usage":{"input_tokens":800,"output_tokens":60,"cache_creation_input_tokens":0,"cache_read_input_tokens":400}}}
{"type":"assistant","timestamp":"2025-01-15T10:04:00.000Z","message":{"role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"All tests pass."}],"usage":{"input_tokens":900,"output_tokens":20,"cache_creation_input_tokens":0,"cache_read_input_tokens":500}}}
`

// BenchmarkParseSessionFile measures end-to-end session file parsing including
// the TailRead I/O step. Represents the hot path in the agent scanner loop.
func BenchmarkParseSessionFile(b *testing.B) {
	// Repeat the sample to create a realistic multi-KB session file.
	content := strings.Repeat(sampleJSONL, 20)
	f, err := os.CreateTemp(b.TempDir(), "bench-session*.jsonl")
	if err != nil {
		b.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		b.Fatal(err)
	}
	f.Close()
	path := f.Name()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = parser.ParseSessionFile(path)
	}
}

// BenchmarkParseSessionFile_Small benchmarks parsing of a minimal single-turn session.
func BenchmarkParseSessionFile_Small(b *testing.B) {
	f, err := os.CreateTemp(b.TempDir(), "bench-small*.jsonl")
	if err != nil {
		b.Fatal(err)
	}
	if _, err := f.WriteString(sampleJSONL); err != nil {
		b.Fatal(err)
	}
	f.Close()
	path := f.Name()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = parser.ParseSessionFile(path)
	}
}

// BenchmarkParseSessionFile_Large benchmarks the 32KB tail-read path.
// File is large enough that TailRead caps at 32KB.
func BenchmarkParseSessionFile_Large(b *testing.B) {
	// ~400 repetitions of the sample (~96KB) exceeds the 32KB tail cap.
	content := strings.Repeat(sampleJSONL, 400)
	f, err := os.CreateTemp(b.TempDir(), "bench-large*.jsonl")
	if err != nil {
		b.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		b.Fatal(err)
	}
	f.Close()
	path := f.Name()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = parser.ParseSessionFile(path)
	}
}
