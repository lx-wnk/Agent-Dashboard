# TDD Plan: Feature Gap Closeout

**Goal:** Wire three already-built features that are dark due to missing last-mile connections.
No schema changes. All changes are Go source, YAML descriptors, and TypeScript.

**Architecture:** 3 independent, file-disjoint fixes; commit sequence A → B1 → B2 → C.
Single PR is fine; each part is a self-contained commit set.

**Tech Stack:** Go 1.26 (chi, ent ORM, modernc/sqlite) + Vue 3 TypeScript (Vite, pnpm)

**Go test scope:** `cd server && go test ./internal/scheduler/ ./internal/provider/ ./internal/pricing/ ./internal/eval/ ./cmd/serve/`
Never run `go test ./...` — it regenerates the entire `internal/db/ent/` tree and can corrupt it.

**Gotchas:**
- `git commit --no-gpg-sign` — SSH signing hangs mid-session.
- Gopls "undefined"/"internal not allowed" in worktrees are false positives; trust `go build ./...`.
- After any `go test` run verify `server/internal/db/ent/` is clean (`git diff --stat`); restore with `git checkout -- server/internal/db/ent/` if corrupted.

---

## Part A — NL→Cron LLM Fallback

### Why exec-path, not adapter-path

`scheduler` is a declared leaf package (comment at top of `nlcron.go`). The `runAdapterPath` in
`internal/refine/spawner.go` requires an `*ent.Spawner` (DB entity), which would force scheduler to
import `db/ent` — forbidden. The `runExecPath` pattern uses only `os/exec` with `binary="claude"` when
`sp==nil`. The NLCron translator needs a one-shot response (not a stream), so
`exec.CommandContext(ctx, "claude", "-p", prompt).Output()` is sufficient — no channel drain needed.

### Files

| File | Action |
|---|---|
| `server/internal/scheduler/nlcron_llm.go` | create |
| `server/internal/scheduler/nlcron_test.go` | extend (add 3 tests) |
| `server/cmd/serve/di_scheduler.go` | change line 66 (1-line) |

### Step A-1 — Failing tests

Extend `server/internal/scheduler/nlcron_test.go` (package `scheduler`, existing imports cover `context`
and `errors`):

```go
// stubLLM is a fake LLMTranslator for seam testing.
type stubLLM struct {
	called bool
	ret    string
	err    error
}

func (s *stubLLM) TranslateToCron(_ context.Context, _ string) (string, error) {
	s.called = true
	return s.ret, s.err
}

func TestTranslate_LLMFallbackCalled(t *testing.T) {
	// "first of the month" hits dayOfMonthRe — rule path deliberately declines it.
	stub := &stubLLM{ret: "0 0 1 * *"}
	n := NewNLCron(stub)
	got, err := n.Translate(context.Background(), "first of the month")
	if err != nil {
		t.Fatal(err)
	}
	if !stub.called {
		t.Error("LLM must be called when rule-based path declines the phrase")
	}
	if got != "0 0 1 * *" {
		t.Errorf("got %q, want %q", got, "0 0 1 * *")
	}
}

func TestTranslate_LLMNotCalledForRuleHit(t *testing.T) {
	stub := &stubLLM{}
	n := NewNLCron(stub)
	if _, err := n.Translate(context.Background(), "every hour"); err != nil {
		t.Fatal(err)
	}
	if stub.called {
		t.Error("LLM must not be called when rule-based path succeeds")
	}
}

func TestTranslate_LLMReturnsInvalidCronYieldsUnparseable(t *testing.T) {
	stub := &stubLLM{ret: "not a cron expression"}
	n := NewNLCron(stub)
	_, err := n.Translate(context.Background(), "first of the month")
	if !errors.Is(err, ErrUnparseable) {
		t.Errorf("want ErrUnparseable when LLM returns bad cron, got %v", err)
	}
}
```

Run: `cd server && go test ./internal/scheduler/` — all three tests PASS because `NLCron.Translate`
already has the LLM branch at line 75. The gap is the nil translator passed in production DI (line 66
of `di_scheduler.go`). These tests confirm the contract is in place before adding the adapter.

### Step A-2 — Implement ExecLLMTranslator

Create `server/internal/scheduler/nlcron_llm.go`:

```go
package scheduler

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ExecLLMTranslator implements LLMTranslator via a one-shot `claude -p` subprocess.
// It mirrors the exec pattern in internal/refine (runExecPath, nil spawner variant)
// but uses Output() instead of streaming since only the first line is needed.
type ExecLLMTranslator struct{}

// NewExecLLMTranslator returns an ExecLLMTranslator.
func NewExecLLMTranslator() *ExecLLMTranslator { return &ExecLLMTranslator{} }

const cronTranslatePrompt = "Return ONLY a single 5-field POSIX cron expression " +
	"(minute hour dom month dow) for this schedule phrase — no explanation, no other text: %s"

// TranslateToCron runs `claude -p <prompt>` and returns the first non-empty output
// line. The caller (NLCron.Translate) validates and rejects invalid expressions.
func (e *ExecLLMTranslator) TranslateToCron(ctx context.Context, phrase string) (string, error) {
	prompt := fmt.Sprintf(cronTranslatePrompt, phrase)
	out, err := exec.CommandContext(ctx, "claude", "-p", prompt).Output()
	if err != nil {
		return "", fmt.Errorf("nlcron: llm exec: %w", err)
	}
	sc := bufio.NewScanner(strings.NewReader(strings.TrimSpace(string(out))))
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			return line, nil
		}
	}
	return "", fmt.Errorf("nlcron: llm returned empty output")
}
```

Run: `cd server && go test ./internal/scheduler/` — PASS (ExecLLMTranslator is not called by tests,
only stubLLM is used; the tests verify the NLCron interface contract).

### Step A-3 — Wire in DI

In `server/cmd/serve/di_scheduler.go` line 66, replace:

```go
// before:
translator := scheduler.NewNLCron(nil)

// after:
translator := scheduler.NewNLCron(scheduler.NewExecLLMTranslator())
```

Run: `cd server && go build ./...` — PASS.

### Step A-4 — Commit

```
feat: wire LLM fallback into NL-to-cron translator
```

---

## Part B1 — Provider Agent LastActivity

### What is broken

`parseJSONL` in `server/internal/provider/engine.go` never sets `SessionData.LastActivity`.
`merger.CalculateStatus(time.Time{})` sees a ~56-year age → every non-Claude agent shows `idle`.

### Timestamp field and format per fixture

| Provider | YAML path to add | JSON field | Format |
|---|---|---|---|
| Codex | `timestamp: [timestamp]` | top-level `timestamp` | RFC3339 (`"2026-04-19T11:23:41.622Z"`) |
| Gemini | `timestamp: [timestamp]` | top-level `timestamp` | RFC3339 (`"2026-04-19T11:00:20.000Z"`) |
| Junie | `timestamp: [timestampMs]` | top-level `timestampMs` | ms-epoch float64 (`1750000020000`) |
| Pi | `timestamp: [timestamp]` | top-level `timestamp` | RFC3339 (`"2026-06-23T10:00:20.000Z"`) |

Timestamps appear on ALL lines (including header lines not matched by the event filter).
The engine must track the newest timestamp across all lines before the filter gate.

### Files

| File | Action |
|---|---|
| `server/internal/provider/descriptor.go` | add `Timestamp []string` to `ParseSpec` |
| `server/internal/provider/engine.go` | add helper + timestamp extraction + set `LastActivity` |
| `server/internal/provider/providers/codex.yaml` | add `timestamp: [timestamp]` |
| `server/internal/provider/providers/gemini.yaml` | add `timestamp: [timestamp]` |
| `server/internal/provider/providers/junie.yaml` | add `timestamp: [timestampMs]` |
| `server/internal/provider/providers/pi.yaml` | add `timestamp: [timestamp]` |
| `server/internal/provider/engine_test.go` | extend (add 2 LastActivity assertions) |

### Step B1-1 — Failing tests

Extend `server/internal/provider/engine_test.go`:

```go
func TestEngine_CodexLastActivity(t *testing.T) {
	d := codexDescriptor()
	d.Parse.Timestamp = []string{"timestamp"}
	r, err := parseJSONL(d, filepath.Join("testdata", "codex-session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	// Newest timestamp in fixture: "2026-04-19T11:23:41.622Z" (third line).
	want, _ := time.Parse(time.RFC3339Nano, "2026-04-19T11:23:41.622Z")
	if r.Session.LastActivity.IsZero() {
		t.Fatal("LastActivity must not be zero for a session with timestamps")
	}
	if !r.Session.LastActivity.Equal(want) {
		t.Errorf("LastActivity = %v, want %v", r.Session.LastActivity, want)
	}
}

func TestEngine_GeminiLastActivity(t *testing.T) {
	d := geminiDescriptor()
	d.Parse.Timestamp = []string{"timestamp"}
	r, err := parseJSONL(d, filepath.Join("testdata", "gemini-session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	// Newest timestamp in fixture: "2026-04-19T11:00:20.000Z" (fourth line).
	want, _ := time.Parse(time.RFC3339Nano, "2026-04-19T11:00:20.000Z")
	if !r.Session.LastActivity.Equal(want) {
		t.Errorf("LastActivity = %v, want %v", r.Session.LastActivity, want)
	}
}
```

Run: `cd server && go test ./internal/provider/` — FAIL (`d.Parse.Timestamp` field does not exist).

### Step B1-2 — Add Timestamp field to ParseSpec

In `server/internal/provider/descriptor.go`, add one field to `ParseSpec`:

```go
type ParseSpec struct {
	EventFilter *EventFilter `yaml:"eventFilter"`
	Tokens      TokenSpec    `yaml:"tokens"`
	Model       []string     `yaml:"model"`
	Provider    []string     `yaml:"provider"`
	Timestamp   []string     `yaml:"timestamp"` // JSON paths to per-line timestamp field
}
```

`Descriptor.Validate()` needs no change — `Timestamp` is optional.

Run: `cd server && go test ./internal/provider/` — FAIL (field exists, but `LastActivity` still zero).

### Step B1-3 — Implement timestamp extraction in engine.go

Add `parseActivityTimestamp` helper below the existing `matchesFilter` function in
`server/internal/provider/engine.go`:

```go
// parseActivityTimestamp coerces a JSON value to a time.Time.
// Accepts RFC3339/RFC3339Nano strings and millisecond-epoch float64 values (e.g. Junie).
func parseActivityTimestamp(v any) time.Time {
	switch t := v.(type) {
	case string:
		if ts, err := time.Parse(time.RFC3339Nano, t); err == nil {
			return ts.UTC()
		}
		if ts, err := time.Parse(time.RFC3339, t); err == nil {
			return ts.UTC()
		}
	case float64:
		// millisecond epoch (json.Unmarshal decodes all JSON numbers as float64)
		return time.UnixMilli(int64(t)).UTC()
	}
	return time.Time{}
}
```

In `parseJSONL`, add a `lastActivity` accumulator and extraction BEFORE the `matchesFilter` gate:

```go
// add after existing var declarations (in, out, cr, cc, model, prov, cost):
var lastActivity time.Time

// inside the scan loop, after json.Unmarshal succeeds and before matchesFilter:
if len(d.Parse.Timestamp) > 0 {
	if vals := resolveFirst(obj, d.Parse.Timestamp); len(vals) > 0 {
		if ts := parseActivityTimestamp(vals[0]); !ts.IsZero() && ts.After(lastActivity) {
			lastActivity = ts
		}
	}
}
```

Update the returned `SessionData` to include `LastActivity`:

```go
return &EngineResult{
	Session: &parser.SessionData{
		TokenUsage: sdk.TokenUsage{
			InputTokens:         int(in),
			OutputTokens:        int(out),
			CacheReadTokens:     int(cr),
			CacheCreationTokens: int(cc),
		},
		Model:        model,
		LastActivity: lastActivity,
	},
	Provider:   prov,
	InFileCost: cost,
}, nil
```

Run: `cd server && go test ./internal/provider/` — PASS (new tests pass; existing tests unaffected
because `Timestamp` defaults to nil slice = no-op).

### Step B1-4 — Update YAML descriptors

`server/internal/provider/providers/codex.yaml` — add inside `parse:` block after `provider:`:
```yaml
  timestamp: [timestamp]
```

`server/internal/provider/providers/gemini.yaml` — add inside `parse:` block after `model:`:
```yaml
  timestamp: [timestamp]
```

`server/internal/provider/providers/junie.yaml` — add inside `parse:` block after `provider:`:
```yaml
  timestamp: [timestampMs]
```

`server/internal/provider/providers/pi.yaml` — add inside `parse:` block after `provider:`:
```yaml
  timestamp: [timestamp]
```

Do NOT change `enabled: false` in any descriptor.

Run: `cd server && go build ./...` — PASS.

### Step B1-5 — Commit

```
fix: parse provider session timestamps to fix always-idle agent status
```

---

## Part B2 — Codex/Gemini Model Pricing

### Files

| File | Action |
|---|---|
| `server/internal/pricing/pricing.go` | add 4 entries to `modelPricing` |
| `server/internal/pricing/pricing_test.go` | extend (add 3 tests) |

### Step B2-1 — Failing tests

Extend `server/internal/pricing/pricing_test.go` (package `pricing_test`):

```go
func TestHasPricing_ThirdPartyModels(t *testing.T) {
	for _, m := range []string{"gpt-5", "gpt-5-codex", "gemini-2.5-pro", "gemini-2.5-flash"} {
		if !pricing.HasPricing(m) {
			t.Errorf("HasPricing(%q) = false, want true", m)
		}
	}
}

func TestEstimateCost_CodexNonZeroAndDistinctFromDefault(t *testing.T) {
	usage := sdk.TokenUsage{InputTokens: 1_000_000, OutputTokens: 1_000_000}
	got := pricing.EstimateCost(usage, "gpt-5-codex")
	require.Greater(t, got, 0.0)
	// Must NOT silently fall back to the claude-sonnet-4-6 default.
	sonnet := pricing.EstimateCost(usage, "claude-sonnet-4-6")
	if math.Abs(got-sonnet) < 0.001 {
		t.Errorf("gpt-5-codex cost (%f) equals sonnet default (%f) — entry missing?", got, sonnet)
	}
}

func TestEstimateCost_GeminiProNonZeroAndDistinctFromDefault(t *testing.T) {
	usage := sdk.TokenUsage{InputTokens: 1_000_000, OutputTokens: 1_000_000}
	got := pricing.EstimateCost(usage, "gemini-2.5-pro")
	require.Greater(t, got, 0.0)
	sonnet := pricing.EstimateCost(usage, "claude-sonnet-4-6")
	if math.Abs(got-sonnet) < 0.001 {
		t.Errorf("gemini-2.5-pro cost (%f) equals sonnet default (%f) — entry missing?", got, sonnet)
	}
}
```

Add `"math"` to the import block if not present.

Run: `cd server && go test ./internal/pricing/` — FAIL (`HasPricing` returns false for new models).

### Step B2-2 — Add pricing entries

In `server/internal/pricing/pricing.go`, extend `modelPricing` (verify prices at the linked sources
before shipping — the values below are from public list prices as of 2026-06-29):

```go
var modelPricing = map[string]modelPricingEntry{
	// Claude models (existing)
	"claude-opus-4-6":   {15, 75, 1.5, 18.75},
	"claude-opus-4-0":   {15, 75, 1.5, 18.75},
	"claude-sonnet-4-6": {3, 15, 0.3, 3.75},
	"claude-sonnet-4-5": {3, 15, 0.3, 3.75},
	"claude-haiku-4-5":  {0.8, 4, 0.08, 1},

	// OpenAI — source: platform.openai.com/pricing (verify before releasing)
	// Cache read = 50% of input price per OpenAI caching docs; cache write = $0.
	"gpt-5":       {5, 20, 2.5, 0},
	"gpt-5-codex": {5, 20, 2.5, 0}, // fixture model; same rate until separate entry published

	// Google Gemini — source: ai.google.dev/pricing (verify before releasing)
	// Context caching prices omitted (tier-dependent); set once confirmed.
	"gemini-2.5-pro":   {1.25, 10, 0, 0},
	"gemini-2.5-flash": {0.075, 0.30, 0, 0},
}
```

Run: `cd server && go test ./internal/pricing/` — PASS.

### Step B2-3 — Commit

```
feat: add OpenAI and Gemini model pricing entries
```

---

## Part C — Drift Alerts Push Live to the UI

### Files

| File | Action |
|---|---|
| `server/cmd/serve/di_eval.go` | create (extracts callback for testability) |
| `server/cmd/serve/di.go` | replace inline `WithOnDrift` closure (1 line) |
| `server/cmd/serve/di_eval_test.go` | create (2 tests) |
| `src/composables/useEvalMetrics.ts` | extend (add EventSource lifecycle) |
| `src/composables/__tests__/useEvalMetrics.test.ts` | extend (1 test) |

### SSE event name: `eval_drift`

The `TaskBroadcaster` on `/api/tasks/stream` is the correct channel — `schedule_changed` already
flows through it (`di_scheduler.go` line 53-55). No new broadcaster or endpoint needed.

### Step C-1 — Failing backend test

Create `server/cmd/serve/di_eval_test.go` (package `main`):

```go
package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/eval"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

func TestEvalOnDrift_BroadcastsEvalDriftEvent(t *testing.T) {
	b := sse.NewBroadcaster()
	tb := sse.NewTaskBroadcaster(b)
	ch := tb.Subscribe()
	defer tb.Unsubscribe(ch)

	fn := evalOnDrift(tb)
	fn([]eval.DriftFinding{{
		Dim:       eval.Dimension{Stage: "implementation"},
		MetricKey: eval.MetricSuccessRate,
		Direction: eval.DirectionDown,
	}})

	select {
	case frame := <-ch:
		if !bytes.Contains(frame, []byte("eval_drift")) {
			t.Errorf("want eval_drift in SSE frame, got: %s", frame)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("timed out: no SSE frame received")
	}
}

func TestEvalOnDrift_MultipleFindings_SingleBroadcast(t *testing.T) {
	b := sse.NewBroadcaster()
	tb := sse.NewTaskBroadcaster(b)
	ch := tb.Subscribe()
	defer tb.Unsubscribe(ch)

	fn := evalOnDrift(tb)
	fn([]eval.DriftFinding{
		{Dim: eval.Dimension{Stage: "implementation"}, MetricKey: eval.MetricSuccessRate},
		{Dim: eval.Dimension{Stage: "self_review"}, MetricKey: eval.MetricMeanIterations},
	})

	select {
	case frame := <-ch:
		if !bytes.Contains(frame, []byte("eval_drift")) {
			t.Errorf("want eval_drift frame, got: %s", frame)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("timed out: no SSE frame received")
	}
	// Exactly one frame (one broadcast per onDrift call, not per finding).
	select {
	case unexpected := <-ch:
		t.Errorf("want exactly 1 frame, got extra: %s", unexpected)
	case <-time.After(10 * time.Millisecond):
		// correct
	}
}
```

Run: `cd server && go test ./cmd/serve/` — FAIL (`evalOnDrift` undefined).

### Step C-2 — Implement evalOnDrift

Create `server/cmd/serve/di_eval.go`:

```go
package main

import (
	"log/slog"

	"github.com/lx-wnk/agent-dashboard/server/internal/eval"
	"github.com/lx-wnk/agent-dashboard/server/internal/sse"
)

// evalOnDrift returns the onDrift callback wired to the task SSE broadcaster.
// Extracted so it can be tested without constructing the full DI graph.
func evalOnDrift(tb *sse.TaskBroadcaster) func([]eval.DriftFinding) {
	return func(findings []eval.DriftFinding) {
		for _, f := range findings {
			slog.Warn("eval: agent drift detected",
				"stage", f.Dim.Stage, "spawnerID", f.Dim.SpawnerID, "model", f.Dim.Model,
				"metric", f.MetricKey, "direction", f.Direction,
				"baseline", f.BaselineValue, "recent", f.RecentValue, "delta", f.Delta)
		}
		tb.Broadcast(sse.TaskEvent{Type: "eval_drift", Payload: len(findings)})
	}
}
```

Run: `cd server && go test ./cmd/serve/` — PASS.

### Step C-3 — Wire in di.go

In `server/cmd/serve/di.go`, replace the inline `WithOnDrift` closure (around line 405):

```go
// before:
).WithOnDrift(func(findings []eval.DriftFinding) {
    for _, f := range findings {
        slog.Warn("eval: agent drift detected",
            "stage", f.Dim.Stage, "spawnerID", f.Dim.SpawnerID, "model", f.Dim.Model,
            "metric", f.MetricKey, "direction", f.Direction,
            "baseline", f.BaselineValue, "recent", f.RecentValue, "delta", f.Delta)
    }
})

// after:
).WithOnDrift(evalOnDrift(taskBroadcaster))
```

Remove the now-unused `slog` import from `di.go` if it was only used by that closure (check for other
`slog` calls in the file first; it's almost certainly still used elsewhere).

Run: `cd server && go build ./...` — PASS.

### Step C-4 — Failing frontend test

In `src/composables/__tests__/useEvalMetrics.test.ts`, add `MockEventSource` and a new test.
Add at the top of the file (before the existing `localStorage` stub):

```ts
class MockEventSource {
  static instances: MockEventSource[] = []
  onmessage: ((e: MessageEvent) => void) | null = null
  onerror: ((e: Event) => void) | null = null
  readyState = 0
  static CONNECTING = 0
  static OPEN = 1
  static CLOSED = 2
  constructor(public url: string) { MockEventSource.instances.push(this) }
  close() { this.readyState = 2 }
}
```

Add to the `beforeEach` block (before `vi.resetModules()`):
```ts
MockEventSource.instances = []
vi.stubGlobal('EventSource', MockEventSource)
```

Add a new `it` block inside the `describe('useEvalMetrics')` block:

```ts
it('re-fetches alerts on eval_drift SSE event', async () => {
  const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => [] })
  vi.stubGlobal('fetch', fetchMock)

  const { start } = withSetup(() => useEvalMetrics.useEvalMetrics())
  start()
  await flushPromises()

  const callsBefore = fetchMock.mock.calls.length

  // Simulate server pushing eval_drift over /api/tasks/stream.
  const es = MockEventSource.instances[0]
  es.onmessage?.(new MessageEvent('message', {
    data: JSON.stringify({ type: 'eval_drift', payload: 1 }),
  }))
  await flushPromises()

  const driftCallsAfter = (fetchMock.mock.calls as [string][])
    .slice(callsBefore)
    .filter(([url]) => String(url).includes('/api/eval/drift'))
  expect(driftCallsAfter.length).toBeGreaterThan(0)
})
```

Run: `pnpm test src/composables/__tests__/useEvalMetrics.test.ts` — FAIL (no EventSource opened,
`fetchAlertsOnly` not triggered by SSE).

### Step C-5 — Implement SSE subscription in useEvalMetrics

In `src/composables/useEvalMetrics.ts`, add an EventSource lifecycle that listens on
`/api/tasks/stream` (the same stream as `useSchedules`) for `eval_drift` events.

Add module-level variables after the existing `let intervalId` block:

```ts
let driftEventSource: EventSource | null = null
```

Add two functions after `fetchAlertsOnly`:

```ts
function startDriftStream(): void {
  if (driftEventSource)
    return
  driftEventSource = new EventSource('/api/tasks/stream')
  driftEventSource.onmessage = (e) => {
    try {
      const event = JSON.parse(e.data as string) as { type: string }
      if (event.type === 'eval_drift')
        void fetchAlertsOnly()
    }
    catch { /* ignore malformed frames */ }
  }
  driftEventSource.onerror = () => {
    if (driftEventSource?.readyState === EventSource.CLOSED) {
      driftEventSource = null
      // SSE dropped — the 60 s setInterval poll in start() keeps data fresh
    }
  }
}

function stopDriftStream(): void {
  driftEventSource?.close()
  driftEventSource = null
}
```

Extend `start()` to open the stream:
```ts
function start(): void {
  if (intervalId !== null)
    return
  active = true
  void fetchAll()
  intervalId = setInterval(fetchAll, POLL_INTERVAL_MS)
  startDriftStream()
}
```

Extend `stop()` to close it:
```ts
function stop(): void {
  active = false
  if (intervalId !== null) {
    clearInterval(intervalId)
    intervalId = null
  }
  aborter?.abort()
  aborter = null
  stopDriftStream()
}
```

Run: `pnpm test src/composables/__tests__/useEvalMetrics.test.ts` — PASS.

### Step C-6 — Commit

```
feat: broadcast eval_drift SSE event on drift detection
```

---

## Final Verification

```bash
# Go
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server
go build ./...
go test ./internal/scheduler/ ./internal/provider/ ./internal/pricing/ ./internal/eval/ ./cmd/serve/
# Confirm ent tree clean:
git diff --stat -- internal/db/ent/

# Frontend
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard
pnpm test
pnpm typecheck
pnpm lint
```

All must be green before opening the PR.

---

## Task Count Summary

| Part | New files | Changed files | Tests added |
|---|---|---|---|
| A | 1 (`nlcron_llm.go`) | 2 (`nlcron_test.go`, `di_scheduler.go`) | 3 Go |
| B1 | 0 | 6 (`descriptor.go`, `engine.go`, 4 YAMLs, `engine_test.go`) | 2 Go |
| B2 | 0 | 2 (`pricing.go`, `pricing_test.go`) | 3 Go |
| C | 2 (`di_eval.go`, `di_eval_test.go`) | 3 (`di.go`, `useEvalMetrics.ts`, `useEvalMetrics.test.ts`) | 2 Go + 1 TS |
| **Total** | **3** | **13** | **11** |
