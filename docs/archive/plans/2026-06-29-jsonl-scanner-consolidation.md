# Implementation Plan: JSONL Scanner Consolidation

**Goal:** Three packages each maintain their own JSONL-scan decode structs and file-open loops.
Extract one exported `parser.ScanMessages` function that all three consumers migrate to.
Then add basename-collision disambiguation and single-flight cache protection to the usage aggregator.

**Architecture after this plan:**

```
parser.ScanMessages  (new, exported)
  ├── OpenJSONLReader  (existing)
  └── ScanJSONLLines   (existing)
       used by:
         parser.scanFullFileTokenUsage   (migrated, stays internal)
         analytics.readToolCallsFromFile (migrated)
         usage.scanJSONLFile             (migrated)
```

**Tech Stack:** Go 1.26, `go test` scoped to
`./internal/parser/ ./internal/analytics/ ./internal/usage/`.
Never run `go test ./...` — it regenerates the ent tree and can corrupt it.
If ent is accidentally touched: `git checkout -- server/internal/db/ent/`.

Worktree LSP "undefined"/"internal not allowed" errors are false positives.
Trust `go build` and `go test`, not gopls.

All commits use `git commit --no-gpg-sign` (SSH signing hangs in this environment).

---

## Task 1 — Extract `parser.ScanMessages` and green tests

### Files
- `server/internal/parser/messages.go` (new)
- `server/internal/parser/messages_test.go` (new)

### Steps

**1. Write failing tests** in `server/internal/parser/messages_test.go`.
Run `cd server && go test ./internal/parser/` — compilation fails because `ScanMessages`
and `Message` do not exist yet.

```go
package parser_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
)

func writeLines(t *testing.T, lines []string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "s.jsonl")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck
	for _, l := range lines {
		fmt.Fprintln(f, l)
	}
	return p
}

func TestScanMessages_UsageAndTimestamp(t *testing.T) {
	ts := "2026-06-29T10:00:00Z"
	path := writeLines(t, []string{
		`{"type":"assistant","timestamp":"` + ts + `","message":{"role":"assistant","model":"claude-sonnet-4-6","usage":{"input_tokens":100,"output_tokens":50,"cache_creation_input_tokens":10,"cache_read_input_tokens":5}}}`,
	})
	var got []parser.Message
	if err := parser.ScanMessages(path, 0, func(m parser.Message) error {
		got = append(got, m)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 message, got %d", len(got))
	}
	m := got[0]
	if m.Type != "assistant" {
		t.Errorf("Type = %q, want assistant", m.Type)
	}
	if m.Role != "assistant" {
		t.Errorf("Role = %q, want assistant", m.Role)
	}
	if m.Model != "claude-sonnet-4-6" {
		t.Errorf("Model = %q, want claude-sonnet-4-6", m.Model)
	}
	if m.Usage == nil {
		t.Fatal("Usage is nil")
	}
	if m.Usage.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", m.Usage.InputTokens)
	}
	if m.Usage.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want 50", m.Usage.OutputTokens)
	}
	if m.Usage.CacheCreationTokens != 10 {
		t.Errorf("CacheCreationTokens = %d, want 10", m.Usage.CacheCreationTokens)
	}
	if m.Usage.CacheReadTokens != 5 {
		t.Errorf("CacheReadTokens = %d, want 5", m.Usage.CacheReadTokens)
	}
	want, _ := time.Parse(time.RFC3339, ts)
	if !m.Timestamp.Equal(want) {
		t.Errorf("Timestamp = %v, want %v", m.Timestamp, want)
	}
}

func TestScanMessages_CompactBoundaryPassthrough(t *testing.T) {
	path := writeLines(t, []string{
		`{"type":"system","subtype":"compact_boundary"}`,
	})
	var got []parser.Message
	_ = parser.ScanMessages(path, 0, func(m parser.Message) error {
		got = append(got, m)
		return nil
	})
	if len(got) != 1 {
		t.Fatalf("want 1 message, got %d", len(got))
	}
	if got[0].Type != "system" || got[0].Subtype != "compact_boundary" {
		t.Errorf("unexpected type/subtype: %q/%q", got[0].Type, got[0].Subtype)
	}
}

func TestScanMessages_MalformedLinesSkipped(t *testing.T) {
	path := writeLines(t, []string{
		`{not valid json}`,
		`{"type":"assistant","message":{"role":"assistant","usage":{"input_tokens":1}}}`,
	})
	var count int
	_ = parser.ScanMessages(path, 0, func(_ parser.Message) error {
		count++
		return nil
	})
	if count != 1 {
		t.Errorf("want 1 message (malformed skipped), got %d", count)
	}
}

func TestScanMessages_ContentRawJSON(t *testing.T) {
	path := writeLines(t, []string{
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read","id":"tu_1"}]}}`,
	})
	var got []parser.Message
	_ = parser.ScanMessages(path, 0, func(m parser.Message) error {
		got = append(got, m)
		return nil
	})
	if len(got) != 1 || len(got[0].Content) == 0 {
		t.Fatal("Content should carry the raw JSON array")
	}
	var blocks []struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(got[0].Content, &blocks); err != nil {
		t.Fatalf("unmarshal Content: %v", err)
	}
	if len(blocks) != 1 || blocks[0].Name != "Read" {
		t.Errorf("unexpected blocks: %+v", blocks)
	}
}

func TestScanMessages_ErrStopScan(t *testing.T) {
	path := writeLines(t, []string{
		`{"type":"assistant","message":{"role":"assistant"}}`,
		`{"type":"assistant","message":{"role":"assistant"}}`,
		`{"type":"assistant","message":{"role":"assistant"}}`,
	})
	var count int
	err := parser.ScanMessages(path, 0, func(_ parser.Message) error {
		count++
		if count == 1 {
			return parser.ErrStopScan
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("want 1 call before stop, got %d", count)
	}
}

func TestScanMessages_MissingFile(t *testing.T) {
	err := parser.ScanMessages("/no/such/file.jsonl", 0, func(_ parser.Message) error { return nil })
	if err == nil {
		t.Fatal("want error for missing file")
	}
}

func TestScanMessages_AbsentTimestampIsZero(t *testing.T) {
	path := writeLines(t, []string{
		`{"type":"assistant","message":{"role":"assistant"}}`,
	})
	var got parser.Message
	_ = parser.ScanMessages(path, 0, func(m parser.Message) error {
		got = m
		return parser.ErrStopScan
	})
	if !got.Timestamp.IsZero() {
		t.Errorf("want zero Timestamp for absent field, got %v", got.Timestamp)
	}
}

func TestScanMessages_MaxBytesLimitsRead(t *testing.T) {
	// Two assistant lines. maxBytes set to only include the second line (tail).
	line1 := `{"type":"assistant","message":{"role":"assistant","usage":{"input_tokens":999}}}`
	line2 := `{"type":"assistant","message":{"role":"assistant","usage":{"input_tokens":1}}}`
	path := writeLines(t, []string{line1, line2})

	// Only tail the last len(line2)+1 bytes.
	maxBytes := int64(len(line2) + 1)
	var tokens []int
	_ = parser.ScanMessages(path, maxBytes, func(m parser.Message) error {
		if m.Usage != nil {
			tokens = append(tokens, m.Usage.InputTokens)
		}
		return nil
	})
	// Only the tail line should be decoded; 999 must not appear.
	for _, tok := range tokens {
		if tok == 999 {
			t.Errorf("maxBytes tail not respected: decoded first line token count 999")
		}
	}
}
```

**2. Run** `cd server && go test ./internal/parser/` — compilation fails (expected).

**3. Create `server/internal/parser/messages.go`:**

```go
package parser

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/lx-wnk/agent-dashboard/sdk"
)

// Message is the decoded shape of one entry line from a Claude session JSONL file.
// ScanMessages yields one Message per successfully decoded line; malformed lines
// are silently skipped.
type Message struct {
	// Type is the outer entry discriminant (e.g. "assistant", "user", "system").
	Type string
	// Subtype carries secondary discriminants such as "compact_boundary".
	Subtype string
	// Timestamp parsed from the RFC3339Nano timestamp field; zero when absent or unparseable.
	Timestamp time.Time
	// Role is message.role (e.g. "assistant", "user"); empty when the message field is absent.
	Role string
	// Model is message.model; empty when absent.
	Model string
	// Usage is message.usage decoded into sdk.TokenUsage; nil when absent.
	Usage *sdk.TokenUsage
	// Content is message.content as raw JSON; nil when absent.
	Content json.RawMessage
}

// ScanMessages opens path, reads up to maxBytes (0 = whole file), decodes each
// JSONL line into a Message, and calls fn per decoded line. Malformed lines are
// silently skipped. ErrStopScan stops iteration without error; any other fn error
// propagates. The caller must not retain Message.Content beyond the fn call if the
// underlying scanner is reused — copy if needed.
func ScanMessages(path string, maxBytes int64, fn func(m Message) error) error {
	rc, err := OpenJSONLReader(path, maxBytes)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer rc.Close() //nolint:errcheck

	return ScanJSONLLines(rc, func(line []byte) error {
		var outer struct {
			Type      string          `json:"type"`
			Subtype   string          `json:"subtype"`
			Timestamp string          `json:"timestamp"`
			Message   json.RawMessage `json:"message"`
		}
		if err := json.Unmarshal(line, &outer); err != nil {
			return nil // skip malformed outer envelope
		}
		var inner struct {
			Role    string          `json:"role"`
			Model   string          `json:"model"`
			Usage   *usageCounters  `json:"usage"`
			Content json.RawMessage `json:"content"`
		}
		// inner decode failure is non-fatal: some line types have no message field
		_ = json.Unmarshal(outer.Message, &inner)

		var ts time.Time
		if outer.Timestamp != "" {
			if parsed, perr := time.Parse(time.RFC3339Nano, outer.Timestamp); perr == nil {
				ts = parsed
			}
		}

		var usage *sdk.TokenUsage
		if inner.Usage != nil {
			usage = &sdk.TokenUsage{
				InputTokens:         inner.Usage.InputTokens,
				OutputTokens:        inner.Usage.OutputTokens,
				CacheCreationTokens: inner.Usage.CacheCreationTokens,
				CacheReadTokens:     inner.Usage.CacheReadTokens,
			}
		}

		return fn(Message{
			Type:      outer.Type,
			Subtype:   outer.Subtype,
			Timestamp: ts,
			Role:      inner.Role,
			Model:     inner.Model,
			Usage:     usage,
			Content:   inner.Content,
		})
	})
}
```

`usageCounters` is already defined in `parser.go` (unexported, snake_case JSON tags matching
the JSONL format). `sdk.TokenUsage` uses camelCase JSON tags and cannot be used directly as
a decode target for the JSONL `usage` object.

**4. Run** `cd server && go test ./internal/parser/` — all tests pass.

**5. Commit:**
```
git commit --no-gpg-sign -m "feat(parser): export ScanMessages for full-file JSONL decoding"
```

---

## Task 2 — Migrate `parser.scanFullFileTokenUsage` to `ScanMessages`

### Files
- `server/internal/parser/parser.go` (modify)

### Steps

**1. Confirm baseline:**
```
cd server && go test ./internal/parser/
```

**2. Rewrite `scanFullFileTokenUsage`** (currently lines ~329-371 in `parser.go`).
Replace the entire function body with a `ScanMessages` call:

```go
func scanFullFileTokenUsage(path string) (fullScanUsage, error) {
	var total fullScanUsage
	err := ScanMessages(path, 0, func(m Message) error {
		if isCompactBoundaryType(m.Type, m.Subtype) {
			total.hasCompaction = true
			return nil
		}
		if m.Role != "assistant" || m.Usage == nil {
			return nil
		}
		total.TokenUsage.InputTokens += m.Usage.InputTokens
		total.TokenUsage.OutputTokens += m.Usage.OutputTokens
		total.TokenUsage.CacheCreationTokens += m.Usage.CacheCreationTokens
		total.TokenUsage.CacheReadTokens += m.Usage.CacheReadTokens
		return nil
	})
	if err != nil {
		return fullScanUsage{}, fmt.Errorf("scan %s: %w", path, err)
	}
	return total, nil
}
```

**3. Delete the `scanEntry` struct** (lines ~296-303 in `parser.go`) — it is now unused.
`jsonlMessage` (the tail-parse envelope) remains; `usageCounters`, `msgContent`, and
`addUsage` also remain because they are still used in the 32 KB tail-read path.

Note: the byte pre-filter (`bytes.Contains(line, usageMarker)`) that was in the old
`scanFullFileTokenUsage` is intentionally not carried over. `ScanMessages` decodes every
line. The pre-filter was a performance optimization, not a correctness requirement. If a
benchmark confirms regression on very large files, an opt-in pre-filter can be added to
`ScanMessages` later without touching callers.

**4. Run:**
```
cd server && go test ./internal/parser/
```

**5. Commit:**
```
git commit --no-gpg-sign -m "refactor(parser): scanFullFileTokenUsage uses ScanMessages, remove scanEntry"
```

---

## Task 3 — Migrate `analytics.readToolCallsFromFile` to `parser.ScanMessages`

### Files
- `server/internal/analytics/scan.go` (modify)

### Steps

**1. Confirm baseline:**
```
cd server && go test ./internal/analytics/
```

**2. Rewrite `readToolCallsFromFile`** and delete the three private decode types at the
bottom of `scan.go` (`scanEntry`, `scanMessage`, `scanBlock`):

Replace the body of `readToolCallsFromFile` with:

```go
func readToolCallsFromFile(path, sessionID string, from, to time.Time) ([]ToolCall, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxFileSize {
		slog.Warn("analytics: session file exceeds cap, tailing", "path", path, "size", info.Size())
	}

	var calls []ToolCall
	err = parser.ScanMessages(path, maxFileSize, func(m parser.Message) error {
		if m.Role != "assistant" {
			return nil
		}
		if !m.Timestamp.IsZero() {
			if !from.IsZero() && m.Timestamp.Before(from) {
				return nil
			}
			if !to.IsZero() && m.Timestamp.After(to) {
				return nil
			}
		}
		var blocks []struct {
			Type string `json:"type"`
			Name string `json:"name"`
			ID   string `json:"id"`
		}
		if err := json.Unmarshal(m.Content, &blocks); err != nil {
			return nil
		}
		for _, b := range blocks {
			if b.Type != "tool_use" || !ToolNameRE.MatchString(b.Name) {
				continue
			}
			calls = append(calls, ToolCall{
				SessionID: sessionID,
				Name:      b.Name,
				ID:        b.ID,
				Timestamp: m.Timestamp,
			})
		}
		return nil
	})
	return calls, err
}
```

Delete from the bottom of `scan.go`:
```go
// DELETE — replaced by parser.Message
type scanEntry struct { ... }
type scanMessage struct { ... }
type scanBlock struct { ... }
```

The `context` import is no longer needed in this file (it was already unused — present only
because `scanSessionsForTools` is in the same file and imports it). Keep `context` if
`scanSessionsForTools` still needs it; remove only the now-dead decode types.

Analytics uses `maxFileSize` (10 MB cap) as the `maxBytes` argument, so this is NOT a
full-file scan. `ScanMessages` handles both full-file (`maxBytes==0`) and capped reads with
the same API — the maxBytes behaviour is inherited from `OpenJSONLReader`.

**3. Run:**
```
cd server && go test ./internal/analytics/
```

`TestScanSessionsForTools_LinearFixture` must still report exactly 3 tool calls
(Read, Edit, Bash) from the fixture file.

**4. Commit:**
```
git commit --no-gpg-sign -m "refactor(analytics): readToolCallsFromFile uses parser.ScanMessages"
```

---

## Task 4 — Migrate `usage.scanJSONLFile` to `parser.ScanMessages`

### Files
- `server/internal/usage/aggregator.go` (modify)

### Steps

**1. Confirm baseline:**
```
cd server && go test ./internal/usage/
```

**2. Rewrite `scanJSONLFile`** and delete the private `usageEntry` and `usageCnts` types:

```go
func scanJSONLFile(path string, now, cutoff7d, cutoff5h time.Time, w5h, w7d *WindowUsage) error {
	return parser.ScanMessages(path, 0, func(m parser.Message) error {
		if m.Role != "assistant" || m.Usage == nil {
			return nil
		}
		if m.Timestamp.IsZero() || m.Timestamp.After(now) {
			return nil
		}
		tokens := int64(m.Usage.InputTokens + m.Usage.OutputTokens +
			m.Usage.CacheCreationTokens + m.Usage.CacheReadTokens)
		costUSD := pricing.EstimateCost(*m.Usage, m.Model)
		costCents := int64(math.Round(costUSD * 100))
		if !m.Timestamp.Before(cutoff7d) {
			w7d.Tokens += tokens
			w7d.CostCents += costCents
		}
		if !m.Timestamp.Before(cutoff5h) {
			w5h.Tokens += tokens
			w5h.CostCents += costCents
		}
		return nil
	})
}
```

Delete from `aggregator.go`:
```go
// DELETE — replaced by parser.Message / sdk.TokenUsage
type usageEntry struct { ... }
type usageCnts struct { ... }
```

`pricing.EstimateCost` takes `sdk.TokenUsage` by value and a `string` model, matching
`*m.Usage` (dereferenced) and `m.Model`.

The `sessionFileRE` and `scanConfigDir` remain unchanged — they handle the directory walk,
which is separate from per-file JSONL decoding.

**3. Run:**
```
cd server && go test ./internal/usage/
```

**4. Commit:**
```
git commit --no-gpg-sign -m "refactor(usage): scanJSONLFile uses parser.ScanMessages, remove usageEntry/usageCnts"
```

---

## Task 5 — Basename-collision disambiguation in the usage aggregator

### Files
- `server/internal/usage/aggregator.go` (modify `scan` method)
- `server/internal/usage/aggregator_test.go` (add two tests)

### Steps

**1. Write failing tests** — append to `aggregator_test.go`:

```go
// TestAggregate_BasenameCollision asserts that two config dirs sharing a basename
// receive distinct account labels.
func TestAggregate_BasenameCollision(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	// Both dirs have basename ".claude" but live under different parents.
	dirA := filepath.Join(t.TempDir(), ".claude")
	dirB := filepath.Join(t.TempDir(), ".claude")
	for _, d := range []string{dirA, dirB} {
		if err := os.MkdirAll(filepath.Join(d, "projects"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	agg := usage.NewAggregator(usage.Options{
		ConfigDirs: func() []string { return []string{dirA, dirB} },
		Now:        func() time.Time { return now },
	})
	res, err := agg.Aggregate()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Accounts) != 2 {
		t.Fatalf("want 2 accounts, got %d", len(res.Accounts))
	}
	if res.Accounts[0].Label == res.Accounts[1].Label {
		t.Errorf("accounts share label %q after basename collision", res.Accounts[0].Label)
	}
}

// TestAggregate_BasenameUnique asserts that a single dir keeps the bare basename.
func TestAggregate_BasenameUnique(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	dir := filepath.Join(t.TempDir(), ".claude")
	os.MkdirAll(filepath.Join(dir, "projects"), 0o755) //nolint:errcheck

	agg := usage.NewAggregator(usage.Options{
		ConfigDirs: func() []string { return []string{dir} },
		Now:        func() time.Time { return now },
	})
	res, err := agg.Aggregate()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Accounts) != 1 {
		t.Fatalf("want 1 account, got %d", len(res.Accounts))
	}
	if res.Accounts[0].Label != ".claude" {
		t.Errorf("want bare basename .claude, got %q", res.Accounts[0].Label)
	}
}
```

**2. Run** `cd server && go test ./internal/usage/` — `TestAggregate_BasenameCollision` fails
(both accounts receive label ".claude").

**3. Replace the label assignment in `scan`** (currently `Account{Label: filepath.Base(dir)}`):

```go
func (a *Aggregator) scan(now time.Time) (*Result, error) {
	cutoff7d := now.Add(-window7d)
	cutoff5h := now.Add(-window5h)

	dirs := a.opts.ConfigDirs()

	// Count how many dirs share each basename so collisions can be disambiguated.
	baseCount := make(map[string]int, len(dirs))
	for _, dir := range dirs {
		baseCount[filepath.Base(dir)]++
	}

	var total Result
	for _, dir := range dirs {
		base := filepath.Base(dir)
		label := base
		if baseCount[base] > 1 {
			// Include the parent path segment to make colliding basenames unique.
			label = filepath.Base(filepath.Dir(dir)) + "/" + base
		}
		acc := Account{Label: label}
		if err := scanConfigDir(dir, now, cutoff7d, cutoff5h, &acc.W5h, &acc.W7d); err != nil {
			slog.Debug("usage: scan dir skipped", "dir", dir, "err", err)
			continue
		}
		total.Accounts = append(total.Accounts, acc)
		total.W5h.Tokens += acc.W5h.Tokens
		total.W5h.CostCents += acc.W5h.CostCents
		total.W7d.Tokens += acc.W7d.Tokens
		total.W7d.CostCents += acc.W7d.CostCents
	}
	return &total, nil
}
```

**4. Run** `cd server && go test ./internal/usage/` — all pass.

**5. Commit:**
```
git commit --no-gpg-sign -m "fix(usage): disambiguate account labels on basename collision"
```

---

## Task 6 — Single-flight protection against thundering herd on cold cache miss

### Files
- `server/internal/usage/aggregator.go` (modify)
- `server/internal/usage/aggregator_test.go` (add test)

### Steps

**1. Write failing test** — append to `aggregator_test.go`.
Add imports `sync` and `sync/atomic` to the test file's import block.

```go
// TestAggregate_Singleflight asserts that N concurrent cold-cache calls trigger
// exactly one real scan via singleflight de-duplication.
func TestAggregate_Singleflight(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "projects"), 0o755) //nolint:errcheck

	const N = 8
	var scanCount atomic.Int64
	// proceed gates the in-flight scan so all N goroutines block inside Aggregate
	// before the scan completes, making de-duplication observable.
	proceed := make(chan struct{})

	agg := usage.NewAggregator(usage.Options{
		ConfigDirs: func() []string { return []string{dir} },
		Now:        func() time.Time { return time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC) },
		OnScan:     func() { scanCount.Add(1); <-proceed },
	})

	var wg sync.WaitGroup
	wg.Add(N)
	for range N {
		go func() {
			defer wg.Done()
			_, _ = agg.Aggregate()
		}()
	}

	// Allow goroutines to reach Aggregate and block on the singleflight before
	// releasing the scan. 20 ms is generous for goroutine scheduling on all CI platforms.
	time.Sleep(20 * time.Millisecond)
	close(proceed)
	wg.Wait()

	if got := scanCount.Load(); got != 1 {
		t.Errorf("want 1 scan for %d concurrent cold-cache calls, got %d", N, got)
	}
}
```

**2. Run** `cd server && go test ./internal/usage/` — `TestAggregate_Singleflight` fails
(current code triggers N scans).

**3. Add `singleflight.Group` to `Aggregator`** in `aggregator.go`.

Add to import block:
```go
"golang.org/x/sync/singleflight"
```

`golang.org/x/sync v0.21.0` is already in `server/go.mod`; no `go get` needed.

Update the struct:
```go
type Aggregator struct {
	opts     Options
	mu       sync.Mutex
	cached   *Result
	cachedAt time.Time
	group    singleflight.Group
}
```

Replace the `Aggregate` method:
```go
// Aggregate returns cached results if within the 60 s TTL, else re-scans.
// Concurrent cold-cache callers share one in-flight scan via singleflight.
func (a *Aggregator) Aggregate() (*Result, error) {
	now := a.opts.Now()

	a.mu.Lock()
	if a.cached != nil && now.Sub(a.cachedAt) < cacheTTL {
		r := a.cached
		a.mu.Unlock()
		return r, nil
	}
	a.mu.Unlock()

	v, err, _ := a.group.Do("scan", func() (any, error) {
		a.opts.OnScan()
		res, err := a.scan(now)
		if err != nil {
			return nil, err
		}
		a.mu.Lock()
		a.cached = res
		a.cachedAt = now
		a.mu.Unlock()
		return res, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*Result), nil
}
```

`now` captured before `group.Do` is used for both `a.scan(now)` and `cachedAt`. If multiple
goroutines call `opts.Now()` and get slightly different values, only the winning goroutine's
`now` is used — negligible within a 60 s TTL.

**4. Run** `cd server && go test ./internal/usage/` — all tests pass.

**5. Final verify across all three packages:**
```
cd server && go build ./... && go test ./internal/parser/ ./internal/analytics/ ./internal/usage/
```

**6. Commit:**
```
git commit --no-gpg-sign -m "fix(usage): singleflight prevents thundering-herd on cold cache miss"
```

---

## Effort Summary

| Task | Description | Effort |
|---|---|---|
| 1 | Extract `ScanMessages` + 8 tests | 0.5 PD |
| 2 | Migrate `scanFullFileTokenUsage` | 0.25 PD |
| 3 | Migrate `analytics.readToolCallsFromFile` | 0.25 PD |
| 4 | Migrate `usage.scanJSONLFile` | 0.25 PD |
| 5 | Basename-collision disambiguation | 0.5 PD |
| 6 | Singleflight cache protection | 0.5 PD |
| **Total** | | **2.25 PD** |

+20% buffer for unforeseen issues: **~2.75 PD**
