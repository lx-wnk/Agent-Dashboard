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
// assistant turn; output_tokens equals the turn number (1, 2, 3, ...).

func TestScanCompactionBaseline(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		// expected per-epoch sums that should be folded into the baseline
		// (i.e. every epoch EXCEPT the final post-last-compaction one).
		wantInput         int
		wantOutput        int
		wantCacheCreation int
		wantCacheRead     int
		wantHasMarker     bool
	}{
		{
			name:    "no compaction yields zero baseline",
			fixture: "no_compaction.jsonl",
		},
		{
			name:    "single compaction folds first epoch (turns 1-3)",
			fixture: "single_compaction.jsonl",
			// turns 1,2,3 → input 30, output 1+2+3=6, cc 300, cr 3000
			wantInput:         30,
			wantOutput:        6,
			wantCacheCreation: 300,
			wantCacheRead:     3000,
			wantHasMarker:     true,
		},
		{
			name:    "double compaction folds epochs 1 and 2 (turns 1-6)",
			fixture: "double_compaction.jsonl",
			// turns 1..6 → input 60, output 1+..+6=21, cc 600, cr 6000
			wantInput:         60,
			wantOutput:        21,
			wantCacheCreation: 600,
			wantCacheRead:     6000,
			wantHasMarker:     true,
		},
		{
			name:    "compaction at end folds all prior turns (turns 1-3)",
			fixture: "compaction_at_end.jsonl",
			// final epoch is empty → baseline = turns 1,2,3
			wantInput:         30,
			wantOutput:        6,
			wantCacheCreation: 300,
			wantCacheRead:     3000,
			wantHasMarker:     true,
		},
		{
			name:    "missing compactMetadata still resets epoch (turns 1-2)",
			fixture: "missing_compact_metadata.jsonl",
			// turns 1,2 → input 20, output 1+2=3, cc 200, cr 2000
			wantInput:         20,
			wantOutput:        3,
			wantCacheCreation: 200,
			wantCacheRead:     2000,
			wantHasMarker:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := scanCompactionBaseline(fixturePath(t, tt.fixture))
			require.NoError(t, err)
			require.Equal(t, tt.wantInput, b.InputTokens, "input")
			require.Equal(t, tt.wantOutput, b.OutputTokens, "output")
			require.Equal(t, tt.wantCacheCreation, b.CacheCreationTokens, "cacheCreation")
			require.Equal(t, tt.wantCacheRead, b.CacheReadTokens, "cacheRead")
			if tt.wantHasMarker {
				require.Greater(t, b.lastMarkerOffset, int64(0), "expected a marker offset")
			} else {
				require.Zero(t, b.lastMarkerOffset, "expected no marker")
			}
		})
	}
}

// TestParseSessionFile_CompactionOutsideTailWindow is the regression test for
// the actual CI-4 bug: the compact_boundary marker lies far before the 32 KB
// tail window, so the tail parse alone sees only the post-compaction epoch. The
// full-file baseline scan must recover the pre-compaction tokens.
//
// The fixture is laid out so BOTH halves of the total are exactly known:
//
//   - Pre-compaction epoch: P assistant turns (output=5 each) followed by a
//     run of padding turns (output=0 each) whose only job is to push the
//     compact_boundary well past the last 32 KB of the file. Everything before
//     the boundary is folded into the baseline by the full scan; padding turns
//     contribute 0 output so the baseline equals exactly 5*P.
//   - Post-compaction epoch: Q small turns (output=7 each), sized so the WHOLE
//     final epoch fits inside the 32 KB tail. The tail loop resets its counters
//     when it reads the compact_boundary line (which is inside the tail because
//     the post-compaction section is < 32 KB), then sums exactly the Q turns.
//     Tail contribution is therefore deterministic: 7*Q.
//
// Expected total output = 5*P (baseline) + 7*Q (tail), proving the baseline is
// folded into the tail total exactly once.
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
	// lies inside the tail window — making the tail contribution deterministic.
	for i := 0; i < Q; i++ {
		b.WriteString(assistant(postIn, postOut))
		b.WriteByte('\n')
	}
	totalSize := int64(b.Len())

	path := filepath.Join(t.TempDir(), "00000000-0000-0000-0000-000000000000.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o640))

	// Byte-layout invariants that make this a genuine outside-tail exercise:
	//   1. The usage-carrying pre-compaction turns END more than 32 KB before
	//      EOF, so they are unreachable by TailRead and ONLY the full scan can
	//      recover them. (Zero-usage padding fills the gap to the boundary.)
	//   2. The boundary line BEGINS inside the tail window, so the tail loop sees
	//      the compact_boundary reset and counts exactly the post-compaction
	//      epoch — never any padding or pre-compaction usage.
	const tailWindow = 32 * 1024
	tailStart := totalSize - tailWindow
	require.Greater(t, tailStart, preCompactionEnd,
		"pre-compaction usage turns must end > 32 KB before EOF (unreachable by tail)")
	require.Greater(t, boundaryStart, tailStart,
		"boundary line must begin inside the 32 KB tail so the tail loop resets on it")

	// The full-file scan must fold in the pre-compaction epoch (output 5*P) — and
	// crucially the compact_boundary lies far outside the 32 KB tail window. The
	// zero-usage padding contributes nothing, so the baseline is exact.
	baseline, err := scanCompactionBaseline(path)
	require.NoError(t, err)
	require.Equal(t, preOut*P, baseline.OutputTokens, "pre-compaction epoch output (exact)")
	require.Equal(t, preIn*P, baseline.InputTokens, "pre-compaction epoch input (exact)")
	require.Greater(t, baseline.OutputTokens, 0, "baseline must be non-zero")
	require.Greater(t, baseline.lastMarkerOffset, int64(0))

	data, err := ParseSessionFile(path)
	require.NoError(t, err)

	// Exact end-to-end total: baseline (pre-compaction, 5*P) folded on top of the
	// deterministic tail contribution (the whole final epoch, 7*Q). This is the
	// real proof that the outside-tail baseline is added exactly once — a naive
	// tail-only parse would yield only 7*Q, and a double-count would yield more
	// than 5*P + 7*Q.
	wantOutput := preOut*P + postOut*Q
	wantInput := preIn*P + postIn*Q
	require.Equal(t, wantOutput, data.TokenUsage.OutputTokens,
		"total output must be baseline (5*P) + final epoch (7*Q), each counted once")
	require.Equal(t, wantInput, data.TokenUsage.InputTokens,
		"total input must be baseline (10*P) + final epoch (2*Q), each counted once")

	// Sanity: the tail genuinely saw the post-compaction epoch (output > baseline)
	// and the contribution it added equals exactly the final epoch.
	tailContribution := data.TokenUsage.OutputTokens - baseline.OutputTokens
	require.Greater(t, data.TokenUsage.OutputTokens, baseline.OutputTokens,
		"total must include a post-compaction tail contribution")
	require.Positive(t, tailContribution, "tail must see the final epoch")
	require.Equal(t, postOut*Q, tailContribution,
		"tail contribution must equal exactly the final epoch (7*Q)")
}

func TestScanCompactionBaseline_MissingFile(t *testing.T) {
	_, err := scanCompactionBaseline(filepath.Join(t.TempDir(), "nope.jsonl"))
	require.Error(t, err)
}

// BenchmarkScanCompactionBaseline measures the full-file scan cost — the only
// added I/O on a cache miss. Risk row 1 in the CI-4 design caps acceptable
// latency at ~200 ms for an 18 MB file; this benchmarks a comparable size.
func BenchmarkScanCompactionBaseline(b *testing.B) {
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
		if _, err := scanCompactionBaseline(path); err != nil {
			b.Fatal(err)
		}
	}
}

// TestFindSessionForProject_CompactionCachedThenReScanned verifies the SSE
// caching contract for compacted sessions: the corrected total survives a
// cache-hit second call, and appending a new compaction triggers a re-scan that
// folds the new epoch in. The compacted file places its boundary outside the
// tail window so only the full scan can recover the early epoch.
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
	// Epoch 1: 3 turns, output 5 each = 15 → must land in the baseline.
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
		"first parse must include the baseline (15) plus tail turns")

	// Cache hit: identical totals, no re-scan needed.
	second, err := FindSessionForProject(cwd, 0, configDir)
	require.NoError(t, err)
	require.Equal(t, first.TokenUsage.OutputTokens, second.TokenUsage.OutputTokens,
		"cache hit must return the corrected total unchanged")

	// Append a second compaction + a turn. mtime bumps → cache miss → re-scan
	// must now fold the second epoch into the baseline too.
	b.WriteString(boundary + "\n")
	b.WriteString(assistant(2) + "\n")
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o640))
	future := time.Now().Add(time.Second)
	require.NoError(t, os.Chtimes(path, future, future))

	third, err := FindSessionForProject(cwd, 0, configDir)
	require.NoError(t, err)
	require.Greater(t, third.TokenUsage.OutputTokens, second.TokenUsage.OutputTokens,
		"appending a compaction must increase the corrected total after re-scan")
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
