package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fixturePath resolves a synthetic compaction fixture by name.
func fixturePath(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join("testdata", "compaction", name)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("fixture %s missing: %v", name, err)
	}
	return p
}

// Each fixture turn uses input=10, cache_creation=100, cache_read=1000 per
// assistant turn; output_tokens equals the (global) turn number (1, 2, 3, ...).

// TestScanFullFileTokenUsage verifies the authoritative whole-file token sum.
// Because per-message usage is unaffected by compaction, the total is simply the
// sum of EVERY assistant turn in the file — compaction boundaries do not subtract
// or partition anything. hasCompaction merely records that a boundary was seen.
func TestScanFullFileTokenUsage(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		// expected whole-file sums across ALL assistant turns.
		wantInput         int
		wantOutput        int
		wantCacheCreation int
		wantCacheRead     int
		wantHasCompaction bool
	}{
		{
			name:    "no compaction sums all 5 turns",
			fixture: "no_compaction.jsonl",
			// 5 turns: input 50, output 1+..+5=15, cc 500, cr 5000
			wantInput:         50,
			wantOutput:        15,
			wantCacheCreation: 500,
			wantCacheRead:     5000,
		},
		{
			name:    "single compaction sums all 5 turns",
			fixture: "single_compaction.jsonl",
			// all 5 turns regardless of the boundary: input 50, output 15
			wantInput:         50,
			wantOutput:        15,
			wantCacheCreation: 500,
			wantCacheRead:     5000,
			wantHasCompaction: true,
		},
		{
			name:    "double compaction sums all 8 turns",
			fixture: "double_compaction.jsonl",
			// all 8 turns: input 80, output 1+..+8=36, cc 800, cr 8000
			wantInput:         80,
			wantOutput:        36,
			wantCacheCreation: 800,
			wantCacheRead:     8000,
			wantHasCompaction: true,
		},
		{
			name:    "compaction at end sums all 3 turns",
			fixture: "compaction_at_end.jsonl",
			// final boundary has no turns after it; total is still all 3 turns
			wantInput:         30,
			wantOutput:        6,
			wantCacheCreation: 300,
			wantCacheRead:     3000,
			wantHasCompaction: true,
		},
		{
			name:    "missing compactMetadata still detected and summed (4 turns)",
			fixture: "missing_compact_metadata.jsonl",
			// 4 turns: input 40, output 1+2+3+4=10, cc 400, cr 4000
			wantInput:         40,
			wantOutput:        10,
			wantCacheCreation: 400,
			wantCacheRead:     4000,
			wantHasCompaction: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := scanFullFileTokenUsage(fixturePath(t, tt.fixture))
			require.NoError(t, err)
			require.Equal(t, tt.wantInput, b.InputTokens, "input")
			require.Equal(t, tt.wantOutput, b.OutputTokens, "output")
			require.Equal(t, tt.wantCacheCreation, b.CacheCreationTokens, "cacheCreation")
			require.Equal(t, tt.wantCacheRead, b.CacheReadTokens, "cacheRead")
			require.Equal(t, tt.wantHasCompaction, b.hasCompaction, "hasCompaction")
		})
	}
}

// TestParseSessionFile_CompactionOutsideTailWindow is a regression test for the
// CI-4 bug class: the compact_boundary marker lies far before the 32 KB tail
// window, so the tail parse alone sees only the post-compaction epoch. The
// whole-file token scan must recover the full lifetime total.
//
// Here the FINAL (post-compaction) epoch is kept SMALL (< 32 KB) so the boundary
// itself lands inside the tail window — the classic case the original fix
// handled. See TestParseSessionFile_FinalEpochExceedsTailWindow for the case
// the original partition got wrong.
func TestParseSessionFile_CompactionOutsideTailWindow(t *testing.T) {
	const (
		preOut  = 5  // output_tokens per pre-compaction assistant turn
		postOut = 7  // output_tokens per post-compaction assistant turn
		P       = 3  // pre-compaction turns that carry usage
		Q       = 4  // post-compaction turns (the whole final epoch)
		preIn   = 10 // input_tokens per pre-compaction turn
		postIn  = 2  // input_tokens per post-compaction turn
	)

	assistant := func(in, out int) string {
		return fmt.Sprintf(`{"type":"assistant","timestamp":"2026-06-04T10:00:00.000Z","message":{"role":"assistant","model":"claude-test","content":[{"type":"text","text":"x"}],"usage":{"input_tokens":%d,"output_tokens":%d,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`, in, out)
	}
	// Zero-usage padding turn — present only to grow the pre-compaction region so
	// the boundary is shoved outside the tail window. Contributes nothing to any
	// token sum, keeping the expected totals exact.
	pad := assistant(0, 0)
	boundary := `{"type":"system","subtype":"compact_boundary","content":"Conversation compacted","timestamp":"2026-06-04T10:00:00.000Z","uuid":"335ae7b9-0eee-404e-9c9d-0bc52829a958","compactMetadata":{"trigger":"auto","preTokens":167918,"postTokens":10536}}`

	var b strings.Builder
	// Pre-compaction epoch: P usage-carrying turns.
	for i := 0; i < P; i++ {
		b.WriteString(assistant(preIn, preOut))
		b.WriteByte('\n')
	}
	// Offset just past the last usage-carrying pre-compaction turn. These P turns
	// are the tokens that the tail can never see; the full scan must recover them.
	preCompactionEnd := int64(b.Len())
	// Padding: grow the pre-compaction region to ~64 KB with zero-usage turns so
	// the usage-carrying pre-compaction turns sit far (> 32 KB) before EOF. The
	// boundary stays near EOF (it must, so the small final epoch fits the tail),
	// but everything that matters for the baseline is pushed out of the window.
	for b.Len() < 64*1024 {
		b.WriteString(pad)
		b.WriteByte('\n')
	}
	// The compaction marker — note its byte offset for the inside-tail assertion.
	boundaryStart := int64(b.Len())
	b.WriteString(boundary)
	b.WriteByte('\n')
	// Post-compaction epoch: Q small turns. Their total byte size is deliberately
	// kept well under 32 KB so the ENTIRE final epoch (plus the boundary line)
	// lies inside the tail window.
	for i := 0; i < Q; i++ {
		b.WriteString(assistant(postIn, postOut))
		b.WriteByte('\n')
	}
	totalSize := int64(b.Len())

	path := filepath.Join(t.TempDir(), "00000000-0000-0000-0000-000000000000.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o640))

	// Byte-layout invariants that make this a genuine outside-tail exercise:
	//   1. The usage-carrying pre-compaction turns END more than 32 KB before EOF,
	//      so they are unreachable by TailRead and ONLY the full scan can recover
	//      them. (Zero-usage padding fills the gap to the boundary.)
	//   2. The boundary line BEGINS inside the tail window.
	const tailWindow = 32 * 1024
	tailStart := totalSize - tailWindow
	require.Greater(t, tailStart, preCompactionEnd,
		"pre-compaction usage turns must end > 32 KB before EOF (unreachable by tail)")
	require.Greater(t, boundaryStart, tailStart,
		"boundary line must begin inside the 32 KB tail")

	// The whole-file scan must sum every usage-carrying turn regardless of where
	// the boundary or the 32 KB tail boundary fall.
	full, err := scanFullFileTokenUsage(path)
	require.NoError(t, err)
	require.True(t, full.hasCompaction, "scan must record the compaction")
	require.Equal(t, preOut*P+postOut*Q, full.OutputTokens, "whole-file output (exact)")
	require.Equal(t, preIn*P+postIn*Q, full.InputTokens, "whole-file input (exact)")

	data, err := ParseSessionFile(path)
	require.NoError(t, err)

	// Exact end-to-end total: every turn counted exactly once. A naive tail-only
	// parse would yield only 7*Q; the whole-file scan yields 5*P + 7*Q.
	require.Equal(t, preOut*P+postOut*Q, data.TokenUsage.OutputTokens,
		"total output must be the whole-file sum (5*P + 7*Q)")
	require.Equal(t, preIn*P+postIn*Q, data.TokenUsage.InputTokens,
		"total input must be the whole-file sum (10*P + 2*Q)")
}

// TestParseSessionFile_FinalEpochExceedsTailWindow is the regression test for the
// P1 bug found in PR #119 review: when the FINAL (post-last-compaction) epoch is
// itself larger than 32 KB, the tail window starts AFTER the last boundary, so
// the bytes between the boundary and the tail-window start belonged to NEITHER
// the old baseline pass (which dropped the final epoch) NOR the old tail pass
// (which only saw the last 32 KB) — they were lost. With the whole-file scan as
// the authoritative token source, every usage-carrying turn is counted exactly
// once regardless of epoch size.
func TestParseSessionFile_FinalEpochExceedsTailWindow(t *testing.T) {
	const (
		preOut  = 11 // output per pre-compaction usage turn
		postOut = 13 // output per post-compaction usage turn
		preIn   = 10 // input per pre-compaction usage turn
		postIn  = 3  // input per post-compaction usage turn
		P       = 4  // pre-compaction usage-carrying turns
		Q       = 50 // post-compaction usage-carrying turns (spread across > 32 KB)
	)

	usageTurn := func(in, out int) string {
		return fmt.Sprintf(`{"type":"assistant","timestamp":"2026-06-04T10:00:00.000Z","message":{"role":"assistant","model":"claude-test","content":[{"type":"text","text":"x"}],"usage":{"input_tokens":%d,"output_tokens":%d,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`, in, out)
	}
	pad := usageTurn(0, 0)
	boundary := `{"type":"system","subtype":"compact_boundary","content":"Conversation compacted","timestamp":"2026-06-04T10:00:00.000Z","uuid":"335ae7b9-0eee-404e-9c9d-0bc52829a958","compactMetadata":{"trigger":"auto","preTokens":167918,"postTokens":10536}}`

	var b strings.Builder
	// Pre-compaction epoch: P usage-carrying turns.
	for i := 0; i < P; i++ {
		b.WriteString(usageTurn(preIn, preOut))
		b.WriteByte('\n')
	}
	// The single compaction marker, written EARLY so the whole post-compaction
	// epoch that follows can grow well past 32 KB.
	b.WriteString(boundary)
	b.WriteByte('\n')
	boundaryEnd := int64(b.Len())

	// Final epoch: Q usage-carrying turns INTERLEAVED with zero-usage padding so
	// the epoch's total byte size exceeds 32 KB and the usage turns are spread
	// from just after the boundary all the way to EOF. The very first usage turns
	// of the final epoch therefore sit OUTSIDE the 32 KB tail window — exactly the
	// bytes the old partition lost.
	for i := 0; i < Q; i++ {
		b.WriteString(usageTurn(postIn, postOut))
		b.WriteByte('\n')
		// Pad after each usage turn to spread the epoch across the file.
		for j := 0; j < 12; j++ {
			b.WriteString(pad)
			b.WriteByte('\n')
		}
	}
	totalSize := int64(b.Len())

	path := filepath.Join(t.TempDir(), "00000000-0000-0000-0000-000000000000.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o640))

	// Invariant: the final epoch genuinely exceeds the 32 KB tail window, so the
	// tail window starts strictly AFTER the last boundary. This is the precise
	// condition under which the old partition lost the gap bytes.
	const tailWindow = 32 * 1024
	tailStart := totalSize - tailWindow
	require.Greater(t, tailStart, boundaryEnd,
		"final epoch must exceed 32 KB so the tail window starts after the last boundary")

	full, err := scanFullFileTokenUsage(path)
	require.NoError(t, err)
	require.True(t, full.hasCompaction)
	require.Equal(t, preOut*P+postOut*Q, full.OutputTokens, "whole-file output (exact)")
	require.Equal(t, preIn*P+postIn*Q, full.InputTokens, "whole-file input (exact)")

	data, err := ParseSessionFile(path)
	require.NoError(t, err)

	// THE regression assertion: the full lifetime total, every turn once. The old
	// code undercounted the final epoch (it lost the boundary→tail-start gap) and
	// would return less than this.
	require.Equal(t, preOut*P+postOut*Q, data.TokenUsage.OutputTokens,
		"final-epoch-over-32KB total output must equal the whole-file sum")
	require.Equal(t, preIn*P+postIn*Q, data.TokenUsage.InputTokens,
		"final-epoch-over-32KB total input must equal the whole-file sum")
}

// TestParseSessionFile_LongNonCompactedExceedsTailWindow documents the strict
// improvement on long NON-compacted sessions: the old tail-only token sum
// undercounted any session whose messages exceeded 32 KB (it only summed the
// last window). The whole-file scan now counts every turn. There is no
// compaction here at all.
func TestParseSessionFile_LongNonCompactedExceedsTailWindow(t *testing.T) {
	const (
		out = 9
		in  = 4
		N   = 60 // usage turns spread across > 32 KB
	)
	usageTurn := func() string {
		return fmt.Sprintf(`{"type":"assistant","timestamp":"2026-06-04T10:00:00.000Z","message":{"role":"assistant","model":"claude-test","content":[{"type":"text","text":"x"}],"usage":{"input_tokens":%d,"output_tokens":%d,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`, in, out)
	}
	pad := `{"type":"user","timestamp":"2026-06-04T10:00:00.000Z","message":{"role":"user","content":[{"type":"text","text":"padding line with no usage to grow the file well past the tail window"}]}}`

	var b strings.Builder
	for i := 0; i < N; i++ {
		b.WriteString(usageTurn())
		b.WriteByte('\n')
		for j := 0; j < 8; j++ {
			b.WriteString(pad)
			b.WriteByte('\n')
		}
	}
	totalSize := int64(b.Len())
	require.Greater(t, totalSize, int64(32*1024),
		"file must exceed the 32 KB tail window for this to exercise the improvement")

	path := filepath.Join(t.TempDir(), "00000000-0000-0000-0000-000000000000.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o640))

	full, err := scanFullFileTokenUsage(path)
	require.NoError(t, err)
	require.False(t, full.hasCompaction, "no compaction in this fixture")

	data, err := ParseSessionFile(path)
	require.NoError(t, err)
	require.Equal(t, out*N, data.TokenUsage.OutputTokens,
		"long non-compacted session must now count every turn (strict improvement)")
	require.Equal(t, in*N, data.TokenUsage.InputTokens)

	// Sanity: a tail-only sum would have undercounted (the file is > 32 KB so the
	// tail window holds fewer than N usage turns).
	tail, err := TailRead(path)
	require.NoError(t, err)
	require.Less(t, tailOnlyTokenUsage(tail).OutputTokens, data.TokenUsage.OutputTokens,
		"tail-only sum must be strictly less than the whole-file sum here")
}

func TestScanFullFileTokenUsage_MissingFile(t *testing.T) {
	_, err := scanFullFileTokenUsage(filepath.Join(t.TempDir(), "nope.jsonl"))
	require.Error(t, err)
}

// BenchmarkScanFullFileTokenUsage measures the full-file scan cost — the only
// added I/O on a cache miss. Risk row 1 in the CI-4 design caps acceptable
// latency at ~200 ms for an 18 MB file; this benchmarks a comparable size.
func BenchmarkScanFullFileTokenUsage(b *testing.B) {
	line := `{"type":"assistant","timestamp":"2026-06-04T10:00:00.000Z","message":{"role":"assistant","model":"claude-test","content":[{"type":"text","text":"x"}],"usage":{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":100,"cache_read_input_tokens":1000}}}` + "\n"
	var sb strings.Builder
	for sb.Len() < 18*1024*1024 {
		sb.WriteString(line)
	}
	path := filepath.Join(b.TempDir(), "00000000-0000-0000-0000-000000000000.jsonl")
	if err := os.WriteFile(path, []byte(sb.String()), 0o640); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := scanFullFileTokenUsage(path); err != nil {
			b.Fatal(err)
		}
	}
}

// TestFindSessionForProject_CompactionCachedThenReScanned verifies the SSE
// caching contract for compacted sessions: the corrected total survives a
// cache-hit second call, and appending a new compaction triggers a re-scan that
// folds the new turns in. The compacted file places its boundary outside the
// tail window so only the full scan can recover the early turns.
func TestFindSessionForProject_CompactionCachedThenReScanned(t *testing.T) {
	orig := SessionCacheTTL
	SessionCacheTTL = 10 * time.Minute
	t.Cleanup(func() { SessionCacheTTL = orig })

	cwd := "/tmp/test-project-compaction-cache"
	configDir := t.TempDir()
	dir := filepath.Join(configDir, "projects", EncodePath(cwd))
	require.NoError(t, os.MkdirAll(dir, 0o750))
	path := filepath.Join(dir, "00000000-0000-0000-0000-000000000000.jsonl")

	now := time.Now().UTC().Format(time.RFC3339Nano)
	assistant := func(out int) string {
		return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"role":"assistant","model":"claude-test","content":[{"type":"text","text":"x"}],"usage":{"input_tokens":10,"output_tokens":%d,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`, now, out)
	}
	boundary := fmt.Sprintf(`{"type":"system","subtype":"compact_boundary","timestamp":%q,"uuid":"335ae7b9-0eee-404e-9c9d-0bc52829a958","compactMetadata":{"trigger":"auto","preTokens":1,"postTokens":1}}`, now)

	var b strings.Builder
	// Epoch 1: 3 turns, output 5 each = 15.
	for i := 0; i < 3; i++ {
		b.WriteString(assistant(5) + "\n")
	}
	b.WriteString(boundary + "\n")
	// Final epoch padded past the 32 KB tail so the boundary is unreachable by
	// the tail read; output 1 each.
	for b.Len() < 64*1024 {
		b.WriteString(assistant(1) + "\n")
	}
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o640))

	first, err := FindSessionForProject(cwd, 0, configDir)
	require.NoError(t, err)
	require.GreaterOrEqual(t, first.TokenUsage.OutputTokens, 15+1,
		"first parse must include the early epoch (15) plus tail turns")

	// Cache hit: identical totals, no re-scan needed.
	second, err := FindSessionForProject(cwd, 0, configDir)
	require.NoError(t, err)
	require.Equal(t, first.TokenUsage.OutputTokens, second.TokenUsage.OutputTokens,
		"cache hit must return the corrected total unchanged")

	// Append a second compaction + a turn. mtime bumps → cache miss → re-scan
	// must now sum the new turn too.
	b.WriteString(boundary + "\n")
	b.WriteString(assistant(2) + "\n")
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o640))
	future := time.Now().Add(time.Second)
	require.NoError(t, os.Chtimes(path, future, future))

	third, err := FindSessionForProject(cwd, 0, configDir)
	require.NoError(t, err)
	require.Greater(t, third.TokenUsage.OutputTokens, second.TokenUsage.OutputTokens,
		"appending a turn must increase the corrected total after re-scan")
}

func TestParseSessionFile_Compaction(t *testing.T) {
	tests := []struct {
		name              string
		fixture           string
		wantInput         int
		wantOutput        int
		wantCacheCreation int
		wantCacheRead     int
	}{
		{
			name:    "no compaction: naive sum of all 5 turns",
			fixture: "no_compaction.jsonl",
			// 5 turns: input 50, output 1+..+5=15, cc 500, cr 5000
			wantInput:         50,
			wantOutput:        15,
			wantCacheCreation: 500,
			wantCacheRead:     5000,
		},
		{
			name:              "single compaction: all 5 turns counted once",
			fixture:           "single_compaction.jsonl",
			wantInput:         50,
			wantOutput:        15,
			wantCacheCreation: 500,
			wantCacheRead:     5000,
		},
		{
			name:    "double compaction: all 8 turns counted once",
			fixture: "double_compaction.jsonl",
			// 8 turns: input 80, output 1+..+8=36, cc 800, cr 8000
			wantInput:         80,
			wantOutput:        36,
			wantCacheCreation: 800,
			wantCacheRead:     8000,
		},
		{
			name:              "compaction at end: 3 turns",
			fixture:           "compaction_at_end.jsonl",
			wantInput:         30,
			wantOutput:        6,
			wantCacheCreation: 300,
			wantCacheRead:     3000,
		},
		{
			name:    "missing compactMetadata: 4 turns counted once",
			fixture: "missing_compact_metadata.jsonl",
			// 4 turns: input 40, output 1+2+3+4=10, cc 400, cr 4000
			wantInput:         40,
			wantOutput:        10,
			wantCacheCreation: 400,
			wantCacheRead:     4000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := ParseSessionFile(fixturePath(t, tt.fixture))
			require.NoError(t, err)
			require.Equal(t, tt.wantInput, data.TokenUsage.InputTokens, "input")
			require.Equal(t, tt.wantOutput, data.TokenUsage.OutputTokens, "output")
			require.Equal(t, tt.wantCacheCreation, data.TokenUsage.CacheCreationTokens, "cacheCreation")
			require.Equal(t, tt.wantCacheRead, data.TokenUsage.CacheReadTokens, "cacheRead")
		})
	}
}
