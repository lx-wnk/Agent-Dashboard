# Usage Consumption Windows Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the permanently dead `/api/quota` (reads a file Claude never writes) with `/api/usage`, a JSONL-derived rolling-window token and cost aggregator covering 5h (session-equivalent) and 7d (weekly-equivalent) windows with optional soft budgets.

**Architecture:** A new `server/internal/usage` package scans Claude config-dir session JSONLs (mtime ≤ 7d prefilter, per-dir grouping) using `parser.OpenJSONLReader` + `parser.ScanJSONLLines`, sums tokens and USD cost via `pricing.EstimateCost`, caches results 60 s behind an injectable clock seam, and exposes them at `GET /api/usage`. Two optional `app_setting` budget keys (`usage.budget.session`, `usage.budget.weekly`) registered in the existing settings registry produce a `pct` when set; the Vue status-bar segment shows a worst-case bar or compact consumption text accordingly.

**Tech Stack:** Go 1.26 backend, ent ORM, Vue 3 TS frontend, go test, vitest

---

## Worktree / LSP note

gopls in dashboard worktrees emits false "undefined"/"internal not allowed" errors because `go.work` maps only the primary repo. Trust `go build` and `go test` — not LSP diagnostics.

## Go test scope rule

**NEVER** run `go test ./...` — it regenerates and can corrupt `server/internal/db/ent/`. Scope every `go test` to the touched package only. If `server/internal/db/ent/` is accidentally regenerated, restore it: `git checkout -- server/internal/db/ent/`.

---

## Task 1 — Usage aggregator package (TDD)

### Files

```
server/internal/usage/aggregator.go       (new)
server/internal/usage/aggregator_test.go  (new)
```

- [ ] **1.1 Write failing tests**

```go
// server/internal/usage/aggregator_test.go
package usage_test

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/usage"
)

func makeProjectDir(t *testing.T, configDir string) string {
	t.Helper()
	p := filepath.Join(configDir, "projects", "enc-proj-1")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeJSONL(t *testing.T, path string, lines []string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck
	for _, l := range lines {
		fmt.Fprintln(f, l)
	}
}

func assistantLine(ts time.Time, model string, input, output int) string {
	type usage struct {
		Input       int `json:"input_tokens"`
		Output      int `json:"output_tokens"`
		CacheCreate int `json:"cache_creation_input_tokens"`
		CacheRead   int `json:"cache_read_input_tokens"`
	}
	type msg struct {
		Role  string `json:"role"`
		Model string `json:"model"`
		Usage usage  `json:"usage"`
	}
	type entry struct {
		Timestamp string `json:"timestamp"`
		Message   msg    `json:"message"`
	}
	b, _ := json.Marshal(entry{
		Timestamp: ts.UTC().Format(time.RFC3339Nano),
		Message:   msg{Role: "assistant", Model: model, Usage: usage{Input: input, Output: output}},
	})
	return string(b)
}

// TestAggregate_Windows asserts messages are bucketed into the correct windows.
func TestAggregate_Windows(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	projDir := makeProjectDir(t, dir)

	inside5h := now.Add(-1 * time.Hour)
	outside5h := now.Add(-6 * time.Hour)

	writeJSONL(t, filepath.Join(projDir, "aaaaaaaa-0000-0000-0000-000000000001.jsonl"), []string{
		assistantLine(inside5h, "claude-sonnet-4-6", 1000, 500),   // 1500 tokens, inside 5h
		assistantLine(outside5h, "claude-sonnet-4-6", 2000, 100),  // 2100 tokens, 7d only
		`{"timestamp":"` + inside5h.UTC().Format(time.RFC3339Nano) + `","message":{"role":"user"}}`, // user msg: skip
		`{this is not valid json}`, // malformed: skip
	})

	agg := usage.NewAggregator(usage.Options{
		ConfigDirs: func() []string { return []string{dir} },
		Now:        func() time.Time { return now },
	})

	res, err := agg.Aggregate()
	if err != nil {
		t.Fatal(err)
	}

	if want := int64(1500); res.W5h.Tokens != want {
		t.Errorf("w5h tokens: got %d, want %d", res.W5h.Tokens, want)
	}
	if want := int64(3600); res.W7d.Tokens != want { // 1500 + 2100
		t.Errorf("w7d tokens: got %d, want %d", res.W7d.Tokens, want)
	}
}

// TestAggregate_Cost asserts cost matches pricing.EstimateCost for a known model.
func TestAggregate_Cost(t *testing.T) {
	// claude-sonnet-4-6: $3/M input + $15/M output
	// 1000 input + 500 output = 0.003 + 0.0075 = 0.0105 USD → 1 cent (rounded)
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	projDir := makeProjectDir(t, dir)

	writeJSONL(t, filepath.Join(projDir, "aaaaaaaa-0000-0000-0000-000000000002.jsonl"), []string{
		assistantLine(now.Add(-1*time.Hour), "claude-sonnet-4-6", 1000, 500),
	})

	agg := usage.NewAggregator(usage.Options{
		ConfigDirs: func() []string { return []string{dir} },
		Now:        func() time.Time { return now },
	})
	res, err := agg.Aggregate()
	if err != nil {
		t.Fatal(err)
	}

	wantCents := int64(math.Round(0.0105 * 100)) // = 1
	if res.W5h.CostCents != wantCents {
		t.Errorf("w5h costCents: got %d, want %d", res.W5h.CostCents, wantCents)
	}
}

// TestAggregate_MultiDir asserts per-dir grouping and correct total.
func TestAggregate_MultiDir(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	dirA := t.TempDir()
	dirB := t.TempDir()

	writeJSONL(t, filepath.Join(dirA, "projects", "p1", "aaaaaaaa-0000-0000-0000-000000000003.jsonl"), []string{
		assistantLine(now.Add(-30*time.Minute), "claude-sonnet-4-6", 100, 0),
	})
	if err := os.MkdirAll(filepath.Join(dirA, "projects", "p1"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSONL(t, filepath.Join(dirB, "projects", "p2", "aaaaaaaa-0000-0000-0000-000000000004.jsonl"), []string{
		assistantLine(now.Add(-30*time.Minute), "claude-sonnet-4-6", 200, 0),
	})
	if err := os.MkdirAll(filepath.Join(dirB, "projects", "p2"), 0o755); err != nil {
		t.Fatal(err)
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
		t.Fatalf("expected 2 accounts, got %d", len(res.Accounts))
	}
	if want := int64(300); res.W5h.Tokens != want {
		t.Errorf("total w5h tokens: got %d, want %d", res.W5h.Tokens, want)
	}
}

// TestAggregate_MtimeFilter asserts that JSONL files older than 7d are skipped.
func TestAggregate_MtimeFilter(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	projDir := makeProjectDir(t, dir)

	p := filepath.Join(projDir, "aaaaaaaa-0000-0000-0000-000000000005.jsonl")
	writeJSONL(t, p, []string{
		assistantLine(now.Add(-1*time.Hour), "claude-sonnet-4-6", 9999, 0),
	})
	oldTime := now.Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(p, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	agg := usage.NewAggregator(usage.Options{
		ConfigDirs: func() []string { return []string{dir} },
		Now:        func() time.Time { return now },
	})
	res, err := agg.Aggregate()
	if err != nil {
		t.Fatal(err)
	}
	if res.W7d.Tokens != 0 {
		t.Errorf("expected 0 tokens from mtime-filtered file, got %d", res.W7d.Tokens)
	}
}

// TestAggregate_Cache asserts the 60 s cache prevents redundant scans.
func TestAggregate_Cache(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "projects", "p"), 0o755) //nolint:errcheck

	scanCount := 0
	agg := usage.NewAggregator(usage.Options{
		ConfigDirs: func() []string { return []string{dir} },
		Now:        func() time.Time { return now },
		OnScan:     func() { scanCount++ },
	})

	if _, err := agg.Aggregate(); err != nil {
		t.Fatal(err)
	}
	if _, err := agg.Aggregate(); err != nil {
		t.Fatal(err)
	}
	if scanCount != 1 {
		t.Errorf("expected 1 scan within 60 s, got %d", scanCount)
	}

	now = now.Add(61 * time.Second) // advance captured variable — closure sees update
	if _, err := agg.Aggregate(); err != nil {
		t.Fatal(err)
	}
	if scanCount != 2 {
		t.Errorf("expected 2 scans after cache expiry, got %d", scanCount)
	}
}
```

Run to confirm failure:

```
cd server && go test ./internal/usage/... 2>&1
# expected: package not found / compilation error
```

- [ ] **1.2 Write implementation**

```go
// server/internal/usage/aggregator.go
package usage

import (
	"encoding/json"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
	"github.com/lx-wnk/agent-dashboard/server/internal/pricing"
)

const (
	window5h = 5 * time.Hour
	window7d = 7 * 24 * time.Hour
	cacheTTL = 60 * time.Second
)

// sessionFileRE matches Claude session JSONL filenames (UUID + .jsonl).
var sessionFileRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.jsonl$`)

// WindowUsage is the token and cost aggregate for one rolling window.
type WindowUsage struct {
	Tokens    int64
	CostCents int64 // USD cost rounded to cents
}

// Account is one config-dir's usage (one Claude account/identity).
type Account struct {
	Label string
	W5h   WindowUsage
	W7d   WindowUsage
}

// Result is the full aggregation output.
type Result struct {
	W5h      WindowUsage
	W7d      WindowUsage
	Accounts []Account
}

// Options controls aggregator behaviour. All fields are optional.
type Options struct {
	// ConfigDirs returns the Claude config directories to scan.
	// Defaults to parser.AllClaudeConfigDirs.
	ConfigDirs func() []string
	// Now returns the current time. Defaults to time.Now.
	Now func() time.Time
	// OnScan is called each time a real scan executes (not a cache hit).
	// Useful for tests to count scans.
	OnScan func()
}

// Aggregator scans session JSONLs and aggregates token usage per rolling window.
type Aggregator struct {
	opts Options

	mu       sync.Mutex
	cached   *Result
	cachedAt time.Time
}

// NewAggregator constructs an Aggregator with optional overrides.
func NewAggregator(opts Options) *Aggregator {
	if opts.ConfigDirs == nil {
		opts.ConfigDirs = parser.AllClaudeConfigDirs
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.OnScan == nil {
		opts.OnScan = func() {}
	}
	return &Aggregator{opts: opts}
}

// Aggregate returns cached results if within the 60 s TTL, else re-scans.
func (a *Aggregator) Aggregate() (*Result, error) {
	now := a.opts.Now()

	a.mu.Lock()
	if a.cached != nil && now.Sub(a.cachedAt) < cacheTTL {
		r := a.cached
		a.mu.Unlock()
		return r, nil
	}
	a.mu.Unlock()

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
}

func (a *Aggregator) scan(now time.Time) (*Result, error) {
	cutoff7d := now.Add(-window7d)
	cutoff5h := now.Add(-window5h)

	var total Result
	for _, dir := range a.opts.ConfigDirs() {
		acc := Account{Label: filepath.Base(dir)}
		if err := scanConfigDir(dir, cutoff7d, cutoff5h, &acc.W5h, &acc.W7d); err != nil {
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

func scanConfigDir(configDir string, cutoff7d, cutoff5h time.Time, w5h, w7d *WindowUsage) error {
	projectsDir := filepath.Join(configDir, "projects")
	projectDirs, err := os.ReadDir(projectsDir)
	if err != nil {
		return err
	}
	for _, pDir := range projectDirs {
		if !pDir.IsDir() {
			continue
		}
		dirPath := filepath.Join(projectsDir, pDir.Name())
		files, err := os.ReadDir(dirPath)
		if err != nil {
			slog.Debug("usage: skip project dir", "path", dirPath, "err", err)
			continue
		}
		for _, f := range files {
			name := f.Name()
			if f.IsDir() || !sessionFileRE.MatchString(name) {
				continue
			}
			info, err := f.Info()
			if err != nil {
				continue
			}
			if info.ModTime().Before(cutoff7d) {
				continue // mtime prefilter: skip files untouched in >7d
			}
			path := filepath.Join(dirPath, name)
			if err := scanJSONLFile(path, cutoff7d, cutoff5h, w5h, w7d); err != nil {
				slog.Debug("usage: skip file", "path", path, "err", err)
			}
		}
	}
	return nil
}

// usageEntry is the JSONL line shape we care about.
type usageEntry struct {
	Timestamp string `json:"timestamp"`
	Message   struct {
		Role  string      `json:"role"`
		Model string      `json:"model"`
		Usage *usageCnts  `json:"usage"`
	} `json:"message"`
}

type usageCnts struct {
	Input       int `json:"input_tokens"`
	Output      int `json:"output_tokens"`
	CacheCreate int `json:"cache_creation_input_tokens"`
	CacheRead   int `json:"cache_read_input_tokens"`
}

func scanJSONLFile(path string, cutoff7d, cutoff5h time.Time, w5h, w7d *WindowUsage) error {
	rc, err := parser.OpenJSONLReader(path, 0) // 0 = read whole file
	if err != nil {
		return err
	}
	defer rc.Close() //nolint:errcheck

	return parser.ScanJSONLLines(rc, func(line []byte) error {
		var e usageEntry
		if err := json.Unmarshal(line, &e); err != nil {
			return nil // malformed line: skip without aborting the scan
		}
		if e.Message.Role != "assistant" || e.Message.Usage == nil {
			return nil
		}

		var ts time.Time
		if e.Timestamp != "" {
			if parsed, err := time.Parse(time.RFC3339Nano, e.Timestamp); err == nil {
				ts = parsed
			}
		}
		if ts.IsZero() {
			return nil // no timestamp: cannot place in window
		}

		u := e.Message.Usage
		tokens := int64(u.Input + u.Output + u.CacheCreate + u.CacheRead)
		costUSD := pricing.EstimateCost(sdk.TokenUsage{
			InputTokens:         u.Input,
			OutputTokens:        u.Output,
			CacheCreationTokens: u.CacheCreate,
			CacheReadTokens:     u.CacheRead,
		}, e.Message.Model)
		costCents := int64(math.Round(costUSD * 100))

		if !ts.Before(cutoff7d) {
			w7d.Tokens += tokens
			w7d.CostCents += costCents
		}
		if !ts.Before(cutoff5h) {
			w5h.Tokens += tokens
			w5h.CostCents += costCents
		}
		return nil
	})
}
```

- [ ] **1.3 Run tests → expect PASS**

```
cd server && go test ./internal/usage/... -v 2>&1
# expect: PASS (5 sub-tests)
```

- [ ] **1.4 Commit**

```
cd server && git commit --no-gpg-sign -m "feat: usage aggregator with 5h/7d rolling windows and 60s cache"
```

---

## Task 2 — Settings registry budget keys

### Files

```
server/internal/settings/registry.go   (edit — add 2 definitions to the list)
```

- [ ] **2.1 Add budget keys**

In `registry.go`, inside the `list := []Definition{...}` slice (after the last existing entry), append:

```go
{Key: "usage.budget.session", Type: TypeInt, Default: "0", Apply: ApplyLive, Category: "usage", validate: nonNegativeInt("usage.budget.session")},
{Key: "usage.budget.weekly", Type: TypeInt, Default: "0", Apply: ApplyLive, Category: "usage", validate: nonNegativeInt("usage.budget.weekly")},
```

`Default: "0"` means unset (no budget). `settingsSvc.Int("usage.budget.session")` returns `0` when unset. The AppSettings.vue auto-groups settings by category and renders TypeInt as an input field — no additional Vue code needed for the settings panel.

- [ ] **2.2 Verify build**

```
cd server && go build ./internal/settings/... 2>&1
```

- [ ] **2.3 Commit**

```
cd server && git commit --no-gpg-sign -m "feat: register usage.budget.session and usage.budget.weekly settings"
```

---

## Task 3 — HTTP handler + route (replace /api/quota with /api/usage)

### Files

```
server/internal/api/usage/handler.go       (new)
server/internal/api/usage/handler_test.go  (new)
server/internal/api/router.go              (edit — swap route + add RouterDeps field)
server/cmd/serve/di.go                     (edit — construct and wire UsageHandler)
```

- [ ] **3.1 Write failing handler test**

```go
// server/internal/api/usage/handler_test.go
package usage_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	apiusage "github.com/lx-wnk/agent-dashboard/server/internal/api/usage"
	"github.com/lx-wnk/agent-dashboard/server/internal/settings"
	"github.com/lx-wnk/agent-dashboard/server/internal/usage"
)

// fakeSettingsRepo satisfies settings.Repo with an in-memory map.
type fakeSettingsRepo struct{ data map[string]string }

func (f *fakeSettingsRepo) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := f.data[key]
	return v, ok, nil
}
func (f *fakeSettingsRepo) Set(_ context.Context, _, _ string) error { return nil }
func (f *fakeSettingsRepo) ListAll(_ context.Context) (map[string]string, error) {
	return f.data, nil
}

func buildHandler(t *testing.T, settingsData map[string]string, configDir string) http.Handler {
	t.Helper()
	svc := settings.New(&fakeSettingsRepo{data: settingsData})
	if err := svc.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	agg := usage.NewAggregator(usage.Options{
		ConfigDirs: func() []string { return []string{configDir} },
		Now:        func() time.Time { return now },
	})
	return apiusage.NewHandler(svc, agg)
}

func writeAssistantLine(t *testing.T, path string, ts time.Time, input, output int) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755) //nolint:errcheck
	line := `{"timestamp":"` + ts.UTC().Format(time.RFC3339Nano) + `","message":{"role":"assistant","model":"claude-sonnet-4-6","usage":{"input_tokens":` +
		itoa(input) + `,"output_tokens":` + itoa(output) + `,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`
	os.WriteFile(path, []byte(line+"\n"), 0o644) //nolint:errcheck
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}

func TestHandler_NoBudget(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	writeAssistantLine(t,
		filepath.Join(dir, "projects", "p", "aaaaaaaa-0000-0000-0000-000000000010.jsonl"),
		now.Add(-30*time.Minute), 500, 200)

	h := buildHandler(t, map[string]string{}, dir)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/usage", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d", rec.Code)
	}
	var resp struct {
		Windows []struct {
			Key          string   `json:"key"`
			Tokens       int64    `json:"tokens"`
			BudgetTokens *int64   `json:"budgetTokens"`
			Pct          *float64 `json:"pct"`
		} `json:"windows"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Windows) != 2 {
		t.Fatalf("expected 2 windows, got %d", len(resp.Windows))
	}
	w5h := resp.Windows[0]
	if w5h.Key != "5h" {
		t.Errorf("first window key: got %q, want %q", w5h.Key, "5h")
	}
	if w5h.Tokens != 700 { // 500 + 200
		t.Errorf("5h tokens: got %d, want 700", w5h.Tokens)
	}
	if w5h.BudgetTokens != nil || w5h.Pct != nil {
		t.Error("expected nil budgetTokens and pct when no budget set")
	}
}

func TestHandler_WithBudget(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	writeAssistantLine(t,
		filepath.Join(dir, "projects", "p", "aaaaaaaa-0000-0000-0000-000000000011.jsonl"),
		now.Add(-30*time.Minute), 1000, 0)

	// session budget = 10000 tokens; pct = 1000/10000 = 0.1
	h := buildHandler(t, map[string]string{"usage.budget.session": "10000"}, dir)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/usage", nil))

	var resp struct {
		Windows []struct {
			Key          string   `json:"key"`
			BudgetTokens *int64   `json:"budgetTokens"`
			Pct          *float64 `json:"pct"`
		} `json:"windows"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	w5h := resp.Windows[0]
	if w5h.BudgetTokens == nil {
		t.Fatal("expected non-nil budgetTokens")
	}
	if *w5h.BudgetTokens != 10000 {
		t.Errorf("budgetTokens: got %d, want 10000", *w5h.BudgetTokens)
	}
	if w5h.Pct == nil {
		t.Fatal("expected non-nil pct")
	}
	if got := *w5h.Pct; got < 0.09 || got > 0.11 {
		t.Errorf("pct: got %f, want ~0.1", got)
	}
}
```

Add missing `fmt` import to handler test file.

Run to confirm failure:
```
cd server && go test ./internal/api/usage/... 2>&1
# expected: package not found
```

- [ ] **3.2 Write handler**

```go
// server/internal/api/usage/handler.go
package usage

import (
	"encoding/json"
	"net/http"

	"github.com/lx-wnk/agent-dashboard/server/internal/settings"
	"github.com/lx-wnk/agent-dashboard/server/internal/usage"
)

// Handler serves GET /api/usage.
type Handler struct {
	agg *usage.Aggregator
	svc *settings.Service
}

// NewHandler constructs the handler. agg may be nil; NewAggregator with default
// Options is used in that case (production path).
func NewHandler(svc *settings.Service, agg *usage.Aggregator) *Handler {
	if agg == nil {
		agg = usage.NewAggregator(usage.Options{})
	}
	return &Handler{agg: agg, svc: svc}
}

type windowDTO struct {
	Key          string   `json:"key"`
	Tokens       int64    `json:"tokens"`
	CostCents    int64    `json:"costCents"`
	BudgetTokens *int64   `json:"budgetTokens"`
	Pct          *float64 `json:"pct"`
}

type windowDetailDTO struct {
	Tokens    int64 `json:"tokens"`
	CostCents int64 `json:"costCents"`
}

type accountDTO struct {
	Label string          `json:"label"`
	W5h   windowDetailDTO `json:"w5h"`
	W7d   windowDetailDTO `json:"w7d"`
}

type responseDTO struct {
	Windows  []windowDTO  `json:"windows"`
	Accounts []accountDTO `json:"accounts,omitempty"`
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	res, err := h.agg.Aggregate()
	if err != nil {
		http.Error(w, "usage scan failed", http.StatusInternalServerError)
		return
	}

	sessionBudget := h.svc.Int("usage.budget.session")
	weeklyBudget := h.svc.Int("usage.budget.weekly")

	resp := responseDTO{
		Windows: []windowDTO{
			makeWindowDTO("5h", res.W5h, sessionBudget),
			makeWindowDTO("7d", res.W7d, weeklyBudget),
		},
	}
	if len(res.Accounts) > 1 {
		for _, acc := range res.Accounts {
			resp.Accounts = append(resp.Accounts, accountDTO{
				Label: acc.Label,
				W5h:   windowDetailDTO{Tokens: acc.W5h.Tokens, CostCents: acc.W5h.CostCents},
				W7d:   windowDetailDTO{Tokens: acc.W7d.Tokens, CostCents: acc.W7d.CostCents},
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func makeWindowDTO(key string, wu usage.WindowUsage, budget int) windowDTO {
	d := windowDTO{Key: key, Tokens: wu.Tokens, CostCents: wu.CostCents}
	if budget > 0 {
		b := int64(budget)
		pct := float64(wu.Tokens) / float64(b)
		if pct > 1 {
			pct = 1
		}
		d.BudgetTokens = &b
		d.Pct = &pct
	}
	return d
}
```

- [ ] **3.3 Wire into router**

In `server/internal/api/router.go`, add to `RouterDeps`:

```go
UsageHandler http.Handler
```

Replace the quota route (line ~266):

```go
// Before:
r.Get("/api/quota", system.Quota)

// After:
if deps.UsageHandler != nil {
    r.Get("/api/usage", deps.UsageHandler.ServeHTTP)
}
```

- [ ] **3.4 Wire into DI**

In `server/cmd/serve/di.go`:

Add import: `apiusage "github.com/lx-wnk/agent-dashboard/server/internal/api/usage"`

Construct the handler after `settingsSvc` is initialized (it is always non-nil in di.go):

```go
usageHandler := apiusage.NewHandler(settingsSvc, nil) // nil agg = uses default scanner
```

Add to `routerDeps`:

```go
UsageHandler: usageHandler,
```

- [ ] **3.5 Run tests → expect PASS**

```
cd server && go test ./internal/api/usage/... -v 2>&1
# expect: PASS
```

- [ ] **3.6 Build check**

```
cd server && go build ./... 2>&1
```

- [ ] **3.7 Commit**

```
cd server && git commit --no-gpg-sign -m "feat: GET /api/usage handler replacing dead /api/quota"
```

---

## Task 4 — Delete dead quota files

### Files

```
server/internal/api/system/quota.go       (delete)
server/internal/api/system/quota_test.go  (delete)
```

- [ ] **4.1 Delete files**

```bash
rm server/internal/api/system/quota.go
rm server/internal/api/system/quota_test.go
```

- [ ] **4.2 Remove stale import in router.go**

Check whether `system.Quota` was the only use of the `system` package alias in `router.go`. The `system` package is still used for `system.HealthHandler`, `system.Config`, `system.System`, so the import stays. Nothing else to change.

- [ ] **4.3 Build + test**

```
cd server && go build ./... 2>&1
cd server && go test ./internal/api/system/... -v 2>&1
```

- [ ] **4.4 Commit**

```
cd server && git commit --no-gpg-sign -m "remove dead quota handler and file-based quota reader"
```

---

## Task 5 — Frontend useUsage composable (TDD)

### Files

```
src/composables/useUsage.ts       (new)
src/composables/useUsage.test.ts  (new)
```

- [ ] **5.1 Write failing test**

```typescript
// src/composables/useUsage.test.ts
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, nextTick } from 'vue'
import { mount } from '@vue/test-utils'

const mockNobudget = {
  windows: [
    { key: '5h', tokens: 2_100_000, costCents: 430, budgetTokens: null, pct: null },
    { key: '7d', tokens: 14_000_000, costCents: 2900, budgetTokens: null, pct: null },
  ],
  accounts: [],
}

const mockWithBudget = {
  windows: [
    { key: '5h', tokens: 1_000_000, costCents: 100, budgetTokens: 10_000_000, pct: 0.1 },
    { key: '7d', tokens: 5_000_000, costCents: 500, budgetTokens: 10_000_000, pct: 0.5 },
  ],
  accounts: [],
}

describe('useUsage', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(mockNobudget),
    }))
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    vi.resetModules()
  })

  it('fetches on start and polls every 5 min', async () => {
    const { useUsage } = await import('./useUsage')
    const Host = defineComponent({
      setup() { const u = useUsage(); u.start(); return u },
      template: '<div />',
    })
    const w = mount(Host)
    await nextTick()
    await vi.runAllTicksAsync()
    expect(fetch).toHaveBeenCalledTimes(1)

    vi.advanceTimersByTime(5 * 60 * 1000)
    await vi.runAllTicksAsync()
    expect(fetch).toHaveBeenCalledTimes(2)

    w.unmount()
  })

  it('clears interval on unmount', async () => {
    const { useUsage } = await import('./useUsage')
    const Host = defineComponent({
      setup() { const u = useUsage(); u.start(); return u },
      template: '<div />',
    })
    const w = mount(Host)
    await vi.runAllTicksAsync()
    w.unmount()
    vi.advanceTimersByTime(10 * 60 * 1000)
    await vi.runAllTicksAsync()
    expect(fetch).toHaveBeenCalledTimes(1) // only the initial fetch
  })

  it('computes worst as nil when no budget', async () => {
    const { useUsage } = await import('./useUsage')
    const Host = defineComponent({
      setup() { const u = useUsage(); u.start(); return u },
      template: '<div />',
    })
    const w = mount(Host)
    await vi.runAllTicksAsync()
    expect((w.vm as any).worst).toBeNull()
    w.unmount()
  })

  it('computes worst as the budgeted window with the highest pct', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(mockWithBudget),
    }))
    vi.resetModules()
    const { useUsage } = await import('./useUsage')
    const Host = defineComponent({
      setup() { const u = useUsage(); u.start(); return u },
      template: '<div />',
    })
    const w = mount(Host)
    await vi.runAllTicksAsync()
    const worst = (w.vm as any).worst
    expect(worst).not.toBeNull()
    expect(worst.key).toBe('7d') // pct 0.5 > 0.1
    w.unmount()
  })
})
```

Run to confirm failure:
```
pnpm test src/composables/useUsage.test.ts 2>&1
# expected: file not found / import error
```

- [ ] **5.2 Write composable**

```typescript
// src/composables/useUsage.ts
import { computed, onUnmounted, ref } from 'vue'

const POLL_INTERVAL_MS = 5 * 60 * 1_000

export interface WindowData {
  key: string
  tokens: number
  costCents: number
  budgetTokens: number | null
  pct: number | null
}

export interface AccountData {
  label: string
  w5h: { tokens: number, costCents: number }
  w7d: { tokens: number, costCents: number }
}

export interface UsageData {
  windows: WindowData[]
  accounts: AccountData[]
}

export function useUsage() {
  const data = ref<UsageData | null>(null)
  let intervalId: ReturnType<typeof setInterval> | null = null
  let aborter: AbortController | null = null

  async function refresh() {
    aborter?.abort()
    aborter = new AbortController()
    try {
      const res = await fetch('/api/usage', { signal: aborter.signal })
      if (!res.ok)
        return
      data.value = await res.json() as UsageData
    }
    catch {
      // AbortError and transient failures: leave last known value
    }
  }

  // worst is the budgeted window with the highest pct; null when no budgets are set.
  const worst = computed<WindowData | null>(() => {
    if (!data.value)
      return null
    const budgeted = data.value.windows.filter(w => w.pct !== null)
    if (budgeted.length === 0)
      return null
    return budgeted.reduce((a, b) => (b.pct! > a.pct! ? b : a))
  })

  function start() {
    if (intervalId !== null)
      return
    void refresh()
    intervalId = setInterval(refresh, POLL_INTERVAL_MS)
  }

  function stop() {
    if (intervalId !== null) {
      clearInterval(intervalId)
      intervalId = null
    }
    aborter?.abort()
    aborter = null
  }

  onUnmounted(stop)

  return { data, worst, refresh, start, stop }
}
```

- [ ] **5.3 Run tests → expect PASS**

```
pnpm test src/composables/useUsage.test.ts 2>&1
```

- [ ] **5.4 Commit**

```
git commit --no-gpg-sign -m "feat: useUsage composable with 5-min polling and worst-case derived"
```

---

## Task 6 — AppStatusBar segment replacement (TDD)

### Files

```
src/components/shell/AppStatusBar.vue      (edit)
src/components/shell/AppStatusBar.test.ts  (edit)
```

- [ ] **6.1 Write failing tests for new USAGE segment**

Replace the existing `AppStatusBar.test.ts` content. The prop `quotaPct: number | null` is removed; `usageData: UsageData | null` is added.

```typescript
// src/components/shell/AppStatusBar.test.ts
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { UsageData } from '../../composables/useUsage'

const store: Record<string, string> = {}
globalThis.localStorage = {
  getItem: (k: string) => store[k] ?? null,
  setItem: (k: string, v: string) => { store[k] = v },
  removeItem: (k: string) => { delete store[k] },
  clear: () => { Object.keys(store).forEach(k => delete store[k]) },
  length: 0,
  key: () => null,
}

vi.mock('../../composables/useSystemResources', () => ({
  useSystemResources: () => ({
    info: {
      value: {
        cpu: { usage: 34, cores: 8, model: 'x' },
        memory: { total: 100, used: 62, available: 38, usagePercent: 62 },
        disk: { total: 100, used: 48, available: 52, usagePercent: 48, mount: '/' },
        loadAvg: [1.2, 1.0, 0.8],
        uptime: 100,
      },
    },
  }),
}))

async function load() {
  vi.resetModules()
  localStorage.clear()
  return (await import('./AppStatusBar.vue')).default
}

const noBudgetUsage: UsageData = {
  windows: [
    { key: '5h', tokens: 2_100_000, costCents: 430, budgetTokens: null, pct: null },
    { key: '7d', tokens: 14_000_000, costCents: 2900, budgetTokens: null, pct: null },
  ],
  accounts: [],
}

const budgetUsage: UsageData = {
  windows: [
    { key: '5h', tokens: 1_000_000, costCents: 100, budgetTokens: 10_000_000, pct: 0.1 },
    { key: '7d', tokens: 5_000_000, costCents: 500, budgetTokens: 10_000_000, pct: 0.5 },
  ],
  accounts: [
    { label: '.claude', w5h: { tokens: 600_000, costCents: 60 }, w7d: { tokens: 3_000_000, costCents: 300 } },
    { label: '.claude-work', w5h: { tokens: 400_000, costCents: 40 }, w7d: { tokens: 2_000_000, costCents: 200 } },
  ],
}

describe('appStatusBar', () => {
  beforeEach(() => localStorage.clear())

  it('renders compact CPU/MEM values in the strip', async () => {
    const Bar = await load()
    const w = mount(Bar, { props: { costDelta: 0.42, todayCostLabel: '$5.00', usageData: null } })
    expect(w.text()).toContain('34%')
    expect(w.text()).toContain('62%')
  })

  it('shows consumption text when no budget is set', async () => {
    const Bar = await load()
    const w = mount(Bar, { props: { costDelta: 0, todayCostLabel: '$0.00', usageData: noBudgetUsage } })
    const seg = w.get('[data-testid="seg-usage"]')
    expect(seg.text()).toContain('2.1M')
    expect(seg.text()).toContain('14.0M')
  })

  it('shows worst-case % bar with SESSION label when 5h is worst', async () => {
    const sessWorst: UsageData = {
      windows: [
        { key: '5h', tokens: 9_000_000, costCents: 900, budgetTokens: 10_000_000, pct: 0.9 },
        { key: '7d', tokens: 1_000_000, costCents: 100, budgetTokens: 10_000_000, pct: 0.1 },
      ],
      accounts: [],
    }
    const Bar = await load()
    const w = mount(Bar, { props: { costDelta: 0, todayCostLabel: '$0.00', usageData: sessWorst } })
    const seg = w.get('[data-testid="seg-usage"]')
    expect(seg.text()).toContain('SESSION')
    expect(seg.text()).toContain('90%')
  })

  it('shows worst-case % bar with WEEKLY label when 7d is worst', async () => {
    const Bar = await load()
    const w = mount(Bar, { props: { costDelta: 0, todayCostLabel: '$0.00', usageData: budgetUsage } })
    const seg = w.get('[data-testid="seg-usage"]')
    expect(seg.text()).toContain('WEEKLY')
    expect(seg.text()).toContain('50%')
  })

  it('opens usage popover on click showing both windows', async () => {
    const Bar = await load()
    const w = mount(Bar, { props: { costDelta: 0, todayCostLabel: '$0.00', usageData: noBudgetUsage } })
    await w.get('[data-testid="seg-usage"]').trigger('click')
    const panel = w.get('[data-testid="panel-usage"]')
    expect(panel.text()).toContain('5h')
    expect(panel.text()).toContain('7d')
  })

  it('shows per-account breakdown in popover when >1 account', async () => {
    const Bar = await load()
    const w = mount(Bar, { props: { costDelta: 0, todayCostLabel: '$0.00', usageData: budgetUsage } })
    await w.get('[data-testid="seg-usage"]').trigger('click')
    const panel = w.get('[data-testid="panel-usage"]')
    expect(panel.text()).toContain('.claude')
    expect(panel.text()).toContain('.claude-work')
  })

  it('collapses to corner tab', async () => {
    const Bar = await load()
    const w = mount(Bar, { props: { costDelta: 0.42, todayCostLabel: '$5.00', usageData: null } })
    await w.get('[data-testid="statusbar-collapse"]').trigger('click')
    expect(w.find('[data-testid="statusbar-tab"]').exists()).toBe(true)
  })

  it('expands cost segment on click', async () => {
    const Bar = await load()
    const w = mount(Bar, { props: { costDelta: 0.42, todayCostLabel: '$5.00', usageData: null } })
    await w.get('[data-testid="seg-cost"]').trigger('click')
    expect(w.find('[data-testid="panel-cost"]').exists()).toBe(true)
  })

  it('renders em-dash when costDelta is null', async () => {
    const Bar = await load()
    const w = mount(Bar, { props: { costDelta: null, todayCostLabel: '$0.00', usageData: null } })
    expect(w.text()).toContain('—')
  })
})
```

Run to confirm failure:
```
pnpm test src/components/shell/AppStatusBar.test.ts 2>&1
```

- [ ] **6.2 Update AppStatusBar.vue**

Replace the `<script setup>` block:

```typescript
<script setup lang="ts">
import type { SystemInfo } from '../../composables/useSystemResources'
import type { UsageData, WindowData } from '../../composables/useUsage'
import { computed } from 'vue'
import { useStatusBar } from '../../composables/useStatusBar'
import { useSystemResources } from '../../composables/useSystemResources'

const props = defineProps<{
  costDelta: number | null
  todayCostLabel: string
  usageData: UsageData | null
}>()

const { collapsed, openSegment, toggleSegment, toggleCollapsed } = useStatusBar()
const resources = useSystemResources()
const systemInfo = computed<SystemInfo | null>(() => resources.info.value)

// worst is the budgeted window with the highest pct; null when no budgets exist.
const worst = computed<WindowData | null>(() => {
  if (!props.usageData)
    return null
  const budgeted = props.usageData.windows.filter(w => w.pct !== null)
  if (budgeted.length === 0)
    return null
  return budgeted.reduce((a, b) => (b.pct! > a.pct! ? b : a))
})

const usageLabel = computed<string>(() => {
  if (!worst.value)
    return ''
  return worst.value.key === '5h' ? 'SESSION' : 'WEEKLY'
})

const usagePctDisplay = computed<string>(() => {
  if (!worst.value || worst.value.pct === null)
    return ''
  return `${Math.round(worst.value.pct * 100)}%`
})

function formatM(tokens: number): string {
  return `${(tokens / 1_000_000).toFixed(1)}M`
}

const usageConsumptionText = computed<string>(() => {
  if (!props.usageData)
    return '—'
  const w5h = props.usageData.windows.find(w => w.key === '5h')
  const w7d = props.usageData.windows.find(w => w.key === '7d')
  if (!w5h || !w7d)
    return '—'
  return `5h ${formatM(w5h.tokens)} · 7d ${formatM(w7d.tokens)}`
})

function barColor(pct: number): string {
  return pct > 85 ? 'bg-danger' : pct > 60 ? 'bg-warning' : 'bg-success'
}

function usageBarColor(): string {
  if (!worst.value || worst.value.pct === null)
    return 'bg-raised'
  const p = worst.value.pct * 100
  return p >= 90 ? 'bg-danger' : p >= 75 ? 'bg-warning' : 'bg-success'
}

function formatDelta(d: number | null): string {
  if (d === null)
    return '—'
  const sign = d < 0 ? '-' : d > 0 ? '+' : ''
  return `${sign}$${Math.abs(d).toFixed(2)}`
}
</script>
```

Replace the QUOTA segment in the template with the USAGE segment. The old segment:

```html
<!-- OLD — remove this -->
<span class="flex items-center gap-1.5" data-testid="seg-quota">
  <span class="text-fg-faint">QUOTA</span>
  <span class="inline-block w-16 h-1.5 bg-raised rounded-full overflow-hidden align-middle">
    <span class="block h-full rounded-full" :class="quotaBarColor(quotaPct)" :style="{ width: quotaPct === null ? '0%' : `${quotaPct}%` }" />
  </span>
  <span class="text-fg">{{ quotaPct === null ? '—' : `${quotaPct}%` }}</span>
</span>
```

Replace with:

```html
<!-- NEW — usage segment (bar when budget set, consumption text otherwise) -->
<button
  type="button"
  data-testid="seg-usage"
  class="flex items-center gap-1.5 hover:text-fg rounded px-1 focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:ring-offset-1 focus-visible:ring-offset-card"
  :aria-expanded="openSegment === 'usage'"
  aria-label="Toggle usage detail"
  @click="toggleSegment('usage')"
>
  <template v-if="worst">
    <span class="text-fg-faint">{{ usageLabel }}</span>
    <span class="inline-block w-16 h-1.5 bg-raised rounded-full overflow-hidden align-middle">
      <span
        class="block h-full rounded-full"
        :class="usageBarColor()"
        :style="{ width: `${Math.round((worst.pct ?? 0) * 100)}%` }"
      />
    </span>
    <span class="text-fg">{{ usagePctDisplay }}</span>
  </template>
  <template v-else>
    <span class="text-fg-faint">USAGE</span>
    <span class="text-fg">{{ usageConsumptionText }}</span>
  </template>
</button>
```

Add the usage popover panel alongside the existing `panel-system` and `panel-cost` panels (before the strip `<div>`):

```html
<div v-if="openSegment === 'usage'" data-testid="panel-usage" class="px-4 py-3 border-b border-line text-[12px] text-fg-mute font-mono">
  <div v-if="usageData" class="flex flex-col gap-1">
    <div v-for="win in usageData.windows" :key="win.key">
      {{ win.key === '5h' ? 'Session (5h)' : 'Weekly (7d)' }}:
      {{ formatM(win.tokens) }} tokens · ${{ (win.costCents / 100).toFixed(2) }}
      <span v-if="win.pct !== null"> · {{ Math.round(win.pct * 100) }}%</span>
    </div>
    <template v-if="usageData.accounts.length > 1">
      <div class="mt-1 text-fg-faint">Accounts:</div>
      <div v-for="acc in usageData.accounts" :key="acc.label" class="pl-2">
        {{ acc.label }}: 5h {{ formatM(acc.w5h.tokens) }} · 7d {{ formatM(acc.w7d.tokens) }}
      </div>
    </template>
  </div>
  <div v-else>No data yet</div>
</div>
```

- [ ] **6.3 Run tests → expect PASS**

```
pnpm test src/components/shell/AppStatusBar.test.ts 2>&1
```

- [ ] **6.4 Commit**

```
git commit --no-gpg-sign -m "feat: replace dead QUOTA bar with USAGE consumption segment in status bar"
```

---

## Task 7 — App.vue wiring

### Files

```
src/App.vue   (edit)
```

- [ ] **7.1 Remove quota refs and wire useUsage**

In `src/App.vue`:

1. Remove the `QuotaInfo` interface, `quota` ref, `quotaPct` computed, `fetchQuota` function, and `onMounted(fetchQuota)` (~lines 300–321).

2. Add import:
```typescript
import { useUsage } from './composables/useUsage'
```

3. In `<script setup>`, add near other composable calls:
```typescript
const usageComposable = useUsage()
onMounted(() => usageComposable.start())
```

4. Update the `<AppStatusBar>` usage in the template. Replace:
```html
<AppStatusBar :cost-delta="costDelta" :today-cost-label="todayCostLabel" :quota-pct="quotaPct" />
```
with:
```html
<AppStatusBar :cost-delta="costDelta" :today-cost-label="todayCostLabel" :usage-data="usageComposable.data.value" />
```

- [ ] **7.2 Typecheck and build**

```
pnpm typecheck 2>&1
pnpm build 2>&1
```

- [ ] **7.3 Commit**

```
git commit --no-gpg-sign -m "feat: wire useUsage composable into App.vue, remove dead quota fetch"
```

---

## Task 8 — Final verify

- [ ] **8.1 Backend full check**

```
cd server && go build ./... 2>&1
cd server && go test ./internal/usage/... -v 2>&1
cd server && go test ./internal/api/usage/... -v 2>&1
cd server && go test ./internal/api/system/... -v 2>&1
cd server && go test ./internal/settings/... -v 2>&1
```

- [ ] **8.2 Frontend full check**

```
pnpm test src/composables/useUsage.test.ts src/components/shell/AppStatusBar.test.ts 2>&1
pnpm typecheck 2>&1
pnpm lint 2>&1
```

- [ ] **8.3 Restore ent if accidentally regenerated**

If `server/internal/db/ent/` was modified:
```
git checkout -- server/internal/db/ent/
```

---

## Task 9 — CHANGELOG + docs

### Files

```
CHANGELOG.md   (edit — add entry under [Unreleased])
```

- [ ] **9.1 Add CHANGELOG entry**

Under `## [Unreleased]` → `### Added`:

```
- `GET /api/usage` — rolling-window token and cost aggregator (5h session-equivalent, 7d weekly-equivalent) derived from session JSONLs across all configured Claude config dirs; replaces the permanently dead `/api/quota` endpoint.
- `usage.budget.session` and `usage.budget.weekly` settings (token counts; 0 = unset) to optionally derive a % bar in the status bar.
- Status-bar USAGE segment: worst-case % bar when a budget is set, compact consumption text otherwise; popover shows both windows, per-account breakdown when multiple accounts exist.
```

Under `### Removed`:

```
- `/api/quota` handler and `usage-data/*.json` file reader (Claude Code never writes that file; the endpoint always returned null).
```

- [ ] **9.2 Commit**

```
git commit --no-gpg-sign -m "docs: CHANGELOG entry for usage consumption windows"
```
