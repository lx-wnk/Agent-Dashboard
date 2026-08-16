# Provider Registry Backend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the five hardcoded provider switches with a table-driven `provider` package that loads declarative descriptors, parses the CLI-family providers' JSONL (Codex, Gemini, Junie) into real token/model/cost data, and classifies Ollama-backed models as `$0`.

**Architecture:** A new `server/internal/provider` package owns descriptors, a registry, a declarative JSONL parse engine, and an Ollama cost-classifier. To avoid an import cycle, the provider-dispatch logic currently in `parser/resolver.go` moves into `provider`; `parser` keeps only schema-specific parsers. The registry is constructed at startup and threaded into the merger and scanner. Provider enable-state is read through an injected `EnabledFunc` seam (default: descriptor flag + `DASHBOARD_PROVIDERS_ENABLED` env list); Plan 2 swaps in a DB-backed source. This plan is backend-only and entirely `go test`-verifiable — no DB, API, or UI.

**Tech Stack:** Go 1.26, `gopkg.in/yaml.v3` (add dep), `go:embed`, modernc/sqlite (unaffected here), existing `sdk` + `parser` + `scanner` + `merger` packages.

**Scope boundary:** This is Plan 1 of 2. Plan 2 (`provider_setting` ent table, REST endpoints, Vue Settings panel) builds on the `EnabledFunc` seam defined here. IDE-embedded adapters (Cursor/Copilot-VSCode/Windsurf, `source: custom:*`) are out of scope for both plans — the `Source` field and an `Adapter` interface stub are defined here so the registry accepts them later, but no adapter is implemented.

---

## File Structure

**New files (all under `server/internal/provider/`):**
- `descriptor.go` — descriptor struct tree + YAML tags + `Validate()`.
- `descriptor_test.go` — validation tests.
- `pathresolve.go` — dotted-path-with-`[]` value extractor over decoded JSON.
- `pathresolve_test.go` — path extractor tests.
- `engine.go` — declarative JSONL parse engine (`source: jsonl`).
- `engine_test.go` — engine tests with real captured fixtures.
- `ollama.go` — `/api/tags` classifier + `localIf` evaluation.
- `ollama_test.go` — classifier tests with a faked HTTP endpoint.
- `registry.go` — load built-ins + user dir, enable-gating, dispatch (`DetectProvider`, `ConfigDirs`, `ResolveSession`, `Cost`).
- `registry_test.go` — registry tests.
- `enabled.go` — `EnabledFunc` seam + default impl.
- `adapter.go` — `Adapter` interface stub for `source: custom:*` (no impls).
- `providers/codex.yaml`, `providers/gemini.yaml`, `providers/junie.yaml` — embedded built-in descriptors.
- `embed.go` — `//go:embed providers/*.yaml` FS.
- `testdata/{codex,gemini,junie}-session.jsonl` — captured real session fixtures.

**Modified files:**
- `sdk/types.go` — `Provider` stays a string type; add `ProviderJunie` const + `CostLocal` field on `Agent`.
- `server/internal/parser/resolver.go` — **delete** (logic moves to `provider`); keep `sessionFileCandidate`/`listJSONLByMtime` if still used, else move them too.
- `server/internal/parser/parser.go` — `AllAgentConfigDirs` loses Codex/Gemini stubs (those move to descriptors); keeps Claude dirs only, renamed `ClaudeConfigDirs`.
- `server/internal/scanner/scanner.go` — `DetectProviderFromCommand` delegates to a registry.
- `server/internal/merger/merger.go` — `EstimateCostForProvider` + non-Claude resolve delegate to the registry; honor `CostLocal`.
- `server/internal/config/config.go` — add `ProviderDir` + `ProvidersEnabled` fields.
- `cmd/serve/di.go` — construct the registry, wire into merger + scanner.
- `server/go.mod` — add `gopkg.in/yaml.v3`.

---

## Phase 1 — Descriptor types & validation

### Task 1: Descriptor struct tree

**Files:**
- Create: `server/internal/provider/descriptor.go`
- Test: `server/internal/provider/descriptor_test.go`

- [ ] **Step 1: Add the YAML dependency**

Run:
```bash
cd server && go get gopkg.in/yaml.v3@v3.0.1
```
Expected: `go.mod` gains `gopkg.in/yaml.v3 v3.0.1`.

- [ ] **Step 2: Write the failing validation test**

```go
// server/internal/provider/descriptor_test.go
package provider

import "testing"

func TestValidate_RejectsMissingID(t *testing.T) {
	d := Descriptor{ExeNames: []string{"codex"}, Source: "jsonl"}
	if err := d.Validate(); err == nil {
		t.Fatal("expected error for missing id, got nil")
	}
}

func TestValidate_RejectsUnknownSource(t *testing.T) {
	d := Descriptor{ID: "x", ExeNames: []string{"x"}, Source: "magic"}
	if err := d.Validate(); err == nil {
		t.Fatal("expected error for unknown source, got nil")
	}
}

func TestValidate_RejectsUnknownTokenMode(t *testing.T) {
	d := Descriptor{
		ID: "x", ExeNames: []string{"x"}, Source: "jsonl",
		SessionGlob: "*.jsonl",
		Parse:       ParseSpec{Tokens: TokenSpec{Mode: "weird"}},
	}
	if err := d.Validate(); err == nil {
		t.Fatal("expected error for unknown token mode, got nil")
	}
}

func TestValidate_AcceptsMinimalJSONL(t *testing.T) {
	d := Descriptor{
		ID: "codex", ExeNames: []string{"codex"}, Source: "jsonl",
		SessionGlob: "sessions/**/rollout-*.jsonl",
		Parse:       ParseSpec{Tokens: TokenSpec{Mode: TokenCumulative}},
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("expected valid descriptor, got %v", err)
	}
}

func TestValidate_AcceptsCustomSource(t *testing.T) {
	d := Descriptor{ID: "cursor", ExeNames: []string{"cursor"}, Source: "custom:cursor"}
	if err := d.Validate(); err != nil {
		t.Fatalf("expected custom source valid, got %v", err)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd server && go test ./internal/provider/ -run TestValidate -v`
Expected: FAIL — `undefined: Descriptor`.

- [ ] **Step 4: Write the descriptor types + Validate**

```go
// server/internal/provider/descriptor.go
package provider

import (
	"fmt"
	"strings"
)

// TokenMode selects how token fields aggregate across matching JSONL lines.
type TokenMode string

const (
	// TokenCumulative: the last matching value is the session total (e.g. Codex).
	TokenCumulative TokenMode = "cumulative"
	// TokenPerMessage: sum the value across every matching line (e.g. Gemini, Junie).
	TokenPerMessage TokenMode = "perMessage"
)

// CostRule selects how a session's cost is derived.
type CostRule string

const (
	CostByModel CostRule = "byModel" // pricing-table lookup
	CostInFile  CostRule = "inFile"  // provider supplies cost in the session file
	CostNone    CostRule = "unknown" // always CostUnknown
)

// Descriptor declares one provider. source "jsonl" is fully declarative;
// source "custom:<id>" routes to a registered Adapter (none built in this plan).
type Descriptor struct {
	ID          string        `yaml:"id"`
	DisplayName string        `yaml:"displayName"`
	Enabled     bool          `yaml:"enabled"`
	ExeNames    []string      `yaml:"exeNames"`
	ConfigDir   ConfigDirSpec `yaml:"configDir"`
	SessionGlob string        `yaml:"sessionGlob"`
	Source      string        `yaml:"source"`
	Parse       ParseSpec     `yaml:"parse"`
	Cost        CostSpec      `yaml:"cost"`
}

type ConfigDirSpec struct {
	Env     string `yaml:"env"`
	Default string `yaml:"default"`
}

type ParseSpec struct {
	EventFilter *EventFilter `yaml:"eventFilter"`
	Tokens      TokenSpec    `yaml:"tokens"`
	Model       []string     `yaml:"model"`
	Provider    []string     `yaml:"provider"`
}

// EventFilter restricts token extraction to JSONL lines where Path == Equals.
type EventFilter struct {
	Path   string `yaml:"path"`
	Equals string `yaml:"equals"`
}

type TokenSpec struct {
	Mode        TokenMode `yaml:"mode"`
	Input       []string  `yaml:"input"`
	Output      []string  `yaml:"output"`
	CacheRead   []string  `yaml:"cacheRead"`
	CacheCreate []string  `yaml:"cacheCreate"`
}

type CostSpec struct {
	Rule       CostRule `yaml:"rule"`
	InFilePath []string `yaml:"inFilePath"`
	LocalIf    *LocalIf `yaml:"localIf"`
}

// LocalIf marks a session as local ($0) when the provider matches or the model
// is a currently-installed Ollama tag.
type LocalIf struct {
	ProviderEquals      string `yaml:"providerEquals"`
	OrModelInOllamaTags bool   `yaml:"orModelInOllamaTags"`
}

// IsCustom reports whether the descriptor routes to an Adapter.
func (d Descriptor) IsCustom() bool { return strings.HasPrefix(d.Source, "custom:") }

// AdapterID returns the adapter key for a custom source ("custom:cursor" -> "cursor").
func (d Descriptor) AdapterID() string { return strings.TrimPrefix(d.Source, "custom:") }

// Validate checks structural invariants. A failing descriptor is dropped at
// load time (logged) so a bad file never crashes the scan.
func (d Descriptor) Validate() error {
	if strings.TrimSpace(d.ID) == "" {
		return fmt.Errorf("descriptor: id is required")
	}
	if len(d.ExeNames) == 0 {
		return fmt.Errorf("descriptor %q: exeNames is required", d.ID)
	}
	if d.Source == "jsonl" {
		if d.SessionGlob == "" {
			return fmt.Errorf("descriptor %q: sessionGlob required for source jsonl", d.ID)
		}
		switch d.Parse.Tokens.Mode {
		case "", TokenCumulative, TokenPerMessage:
		default:
			return fmt.Errorf("descriptor %q: unknown token mode %q", d.ID, d.Parse.Tokens.Mode)
		}
		return nil
	}
	if d.IsCustom() {
		return nil
	}
	return fmt.Errorf("descriptor %q: unknown source %q", d.ID, d.Source)
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd server && go test ./internal/provider/ -run TestValidate -v`
Expected: PASS (5 tests).

- [ ] **Step 6: Commit**

```bash
git add server/internal/provider/descriptor.go server/internal/provider/descriptor_test.go server/go.mod server/go.sum
git commit -m "feat: add provider descriptor types and validation"
```

---

## Phase 2 — Path resolver

### Task 2: Dotted-path value extractor

**Files:**
- Create: `server/internal/provider/pathresolve.go`
- Test: `server/internal/provider/pathresolve_test.go`

The engine needs to read values like `payload.info.total_token_usage.input_tokens` and array-iterating paths like `event.agentEvent.modelUsage[].cost` from a decoded `map[string]any`. A path is a list of candidate paths (fallbacks); the first that yields ≥1 value wins.

- [ ] **Step 1: Write the failing test**

```go
// server/internal/provider/pathresolve_test.go
package provider

import (
	"encoding/json"
	"testing"
)

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

func TestResolvePath_Scalar(t *testing.T) {
	m := decode(t, `{"payload":{"info":{"total_token_usage":{"input_tokens":31751}}}}`)
	got := resolvePath(m, "payload.info.total_token_usage.input_tokens")
	if len(got) != 1 || toFloat(got[0]) != 31751 {
		t.Fatalf("want [31751], got %v", got)
	}
}

func TestResolvePath_Missing(t *testing.T) {
	m := decode(t, `{"payload":{}}`)
	if got := resolvePath(m, "payload.info.input"); len(got) != 0 {
		t.Fatalf("want empty, got %v", got)
	}
}

func TestResolvePath_ArrayIteration(t *testing.T) {
	m := decode(t, `{"event":{"agentEvent":{"modelUsage":[{"cost":0.01},{"cost":0.02}]}}}`)
	got := resolvePath(m, "event.agentEvent.modelUsage[].cost")
	if len(got) != 2 || toFloat(got[0]) != 0.01 || toFloat(got[1]) != 0.02 {
		t.Fatalf("want [0.01 0.02], got %v", got)
	}
}

func TestResolveFirst_FallbackList(t *testing.T) {
	m := decode(t, `{"modelUsage":[{"inputTokens":10}]}`)
	// first candidate misses, second hits
	got := resolveFirst(m, []string{"modelUsage[].input", "modelUsage[].inputTokens"})
	if len(got) != 1 || toFloat(got[0]) != 10 {
		t.Fatalf("want [10], got %v", got)
	}
}

func TestToFloat_StringAndNumber(t *testing.T) {
	if toFloat(float64(5)) != 5 || toFloat("7") != 7 || toFloat(nil) != 0 {
		t.Fatal("toFloat conversions wrong")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/provider/ -run 'TestResolve|TestToFloat' -v`
Expected: FAIL — `undefined: resolvePath`.

- [ ] **Step 3: Write the resolver**

```go
// server/internal/provider/pathresolve.go
package provider

import (
	"strconv"
	"strings"
)

// resolveFirst tries each candidate path in order and returns the values from
// the first path that yields at least one. Empty if all miss.
func resolveFirst(root any, paths []string) []any {
	for _, p := range paths {
		if vals := resolvePath(root, p); len(vals) > 0 {
			return vals
		}
	}
	return nil
}

// resolvePath walks a dotted path from root. A segment ending in "[]" descends
// into an array and fans out across its elements. Returns every leaf value
// reached (multiple when an array is traversed).
func resolvePath(root any, path string) []any {
	if path == "" {
		return nil
	}
	cur := []any{root}
	for _, seg := range strings.Split(path, ".") {
		array := strings.HasSuffix(seg, "[]")
		key := strings.TrimSuffix(seg, "[]")
		var next []any
		for _, node := range cur {
			m, ok := node.(map[string]any)
			if !ok {
				continue
			}
			v, ok := m[key]
			if !ok || v == nil {
				continue
			}
			if array {
				if arr, ok := v.([]any); ok {
					next = append(next, arr...)
				}
				continue
			}
			next = append(next, v)
		}
		cur = next
		if len(cur) == 0 {
			return nil
		}
	}
	return cur
}

// toFloat coerces a decoded JSON value (float64, json.Number, or numeric
// string) to float64. Non-numeric or nil yields 0.
func toFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case string:
		f, _ := strconv.ParseFloat(t, 64)
		return f
	default:
		return 0
	}
}

// firstString returns the first resolved value as a string, or "".
func firstString(root any, paths []string) string {
	for _, v := range resolveFirst(root, paths) {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/provider/ -run 'TestResolve|TestToFloat' -v`
Expected: PASS (5 tests).

- [ ] **Step 5: Commit**

```bash
git add server/internal/provider/pathresolve.go server/internal/provider/pathresolve_test.go
git commit -m "feat: add provider field-path resolver with array fan-out"
```

---

## Phase 3 — JSONL parse engine

### Task 3: Capture real session fixtures

**Files:**
- Create: `server/internal/provider/testdata/codex-session.jsonl`
- Create: `server/internal/provider/testdata/gemini-session.jsonl`
- Create: `server/internal/provider/testdata/junie-session.jsonl`

- [ ] **Step 1: Write the Codex fixture** (schema from research: `~/.codex/sessions/**/rollout-*.jsonl`)

```
{"timestamp":"2026-04-19T11:23:40.000Z","type":"session_meta","payload":{"model_provider":"openai","cli_version":"0.130.0"}}
{"timestamp":"2026-04-19T11:23:40.500Z","type":"turn_context","payload":{"model":"gpt-5-codex"}}
{"timestamp":"2026-04-19T11:23:41.622Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":31751,"cached_input_tokens":14720,"output_tokens":2367,"reasoning_output_tokens":413,"total_tokens":34118},"model_context_window":258400}}}
```

- [ ] **Step 2: Write the Gemini fixture** (schema: `~/.gemini/tmp/<hash>/chats/session-*.jsonl`, `type:"gemini"` lines)

```
{"sessionId":"abc123","projectHash":"deadbeef","startTime":"2026-04-19T11:00:00.000Z","kind":"main"}
{"id":"m1","timestamp":"2026-04-19T11:00:05.000Z","type":"user","content":"hi"}
{"id":"m2","timestamp":"2026-04-19T11:00:09.000Z","type":"gemini","model":"gemini-2.5-pro","tokens":{"input":1200,"output":340,"cached":100,"thoughts":50,"tool":0,"total":1690}}
{"id":"m3","timestamp":"2026-04-19T11:00:20.000Z","type":"gemini","model":"gemini-2.5-pro","tokens":{"input":1500,"output":420,"cached":120,"thoughts":60,"tool":0,"total":2100}}
```

- [ ] **Step 3: Write the Junie fixture** (schema: `~/.junie/sessions/<id>/events.jsonl`, in-file cost+provider)

```
{"timestampMs":1750000000000,"kind":"UserPromptEvent","event":{}}
{"timestampMs":1750000005000,"kind":"LlmResponseMetadataEvent","event":{"agentEvent":{"kind":"LlmResponseMetadataEvent","modelUsage":[{"model":"claude-opus-4-6","provider":"anthropic","inputTokens":1234,"outputTokens":456,"cacheInputTokens":100,"cacheCreateTokens":50,"cost":0.0123,"time":3200}]}}}
{"timestampMs":1750000020000,"kind":"LlmResponseMetadataEvent","event":{"agentEvent":{"kind":"LlmResponseMetadataEvent","modelUsage":[{"model":"claude-opus-4-6","provider":"anthropic","inputTokens":800,"outputTokens":300,"cacheInputTokens":40,"cacheCreateTokens":20,"cost":0.0080,"time":2100}]}}}
```

- [ ] **Step 4: Commit**

```bash
git add server/internal/provider/testdata/
git commit -m "test: add captured provider session fixtures"
```

### Task 4: Declarative JSONL engine

**Files:**
- Create: `server/internal/provider/engine.go`
- Test: `server/internal/provider/engine_test.go`

The engine reads a session file line-by-line, applies the optional `eventFilter`, accumulates token fields per `mode`, picks the last model/provider seen, and returns a `*parser.SessionData`. Cost is left to the registry (`Cost`); the engine fills `TokenUsage`, `Model`, and exposes the in-file provider + cost via the returned `EngineResult`.

- [ ] **Step 1: Write the failing test**

```go
// server/internal/provider/engine_test.go
package provider

import (
	"path/filepath"
	"testing"
)

func codexDescriptor() Descriptor {
	return Descriptor{
		ID: "codex", ExeNames: []string{"codex"}, Source: "jsonl",
		SessionGlob: "*.jsonl",
		Parse: ParseSpec{
			EventFilter: &EventFilter{Path: "payload.type", Equals: "token_count"},
			Tokens: TokenSpec{
				Mode:      TokenCumulative,
				Input:     []string{"payload.info.total_token_usage.input_tokens"},
				Output:    []string{"payload.info.total_token_usage.output_tokens"},
				CacheRead: []string{"payload.info.total_token_usage.cached_input_tokens"},
			},
			Model:    []string{"payload.model"},
			Provider: []string{"payload.model_provider"},
		},
	}
}

func TestEngine_CodexCumulative(t *testing.T) {
	r, err := parseJSONL(codexDescriptor(), filepath.Join("testdata", "codex-session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Session.TokenUsage.InputTokens != 31751 || r.Session.TokenUsage.OutputTokens != 2367 {
		t.Fatalf("tokens wrong: %+v", r.Session.TokenUsage)
	}
	if r.Session.TokenUsage.CacheReadTokens != 14720 {
		t.Fatalf("cacheRead wrong: %d", r.Session.TokenUsage.CacheReadTokens)
	}
	if r.Session.Model != "gpt-5-codex" {
		t.Fatalf("model wrong: %q", r.Session.Model)
	}
	if r.Provider != "openai" {
		t.Fatalf("provider wrong: %q", r.Provider)
	}
}

func geminiDescriptor() Descriptor {
	return Descriptor{
		ID: "gemini", ExeNames: []string{"gemini"}, Source: "jsonl",
		SessionGlob: "*.jsonl",
		Parse: ParseSpec{
			EventFilter: &EventFilter{Path: "type", Equals: "gemini"},
			Tokens: TokenSpec{
				Mode:      TokenPerMessage,
				Input:     []string{"tokens.input"},
				Output:    []string{"tokens.output"},
				CacheRead: []string{"tokens.cached"},
			},
			Model: []string{"model"},
		},
	}
}

func TestEngine_GeminiPerMessageSum(t *testing.T) {
	r, err := parseJSONL(geminiDescriptor(), filepath.Join("testdata", "gemini-session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	// 1200 + 1500
	if r.Session.TokenUsage.InputTokens != 2700 {
		t.Fatalf("want 2700 input, got %d", r.Session.TokenUsage.InputTokens)
	}
	// 340 + 420
	if r.Session.TokenUsage.OutputTokens != 760 {
		t.Fatalf("want 760 output, got %d", r.Session.TokenUsage.OutputTokens)
	}
}

func junieDescriptor() Descriptor {
	return Descriptor{
		ID: "junie", ExeNames: []string{"junie"}, Source: "jsonl",
		SessionGlob: "*.jsonl",
		Parse: ParseSpec{
			EventFilter: &EventFilter{Path: "kind", Equals: "LlmResponseMetadataEvent"},
			Tokens: TokenSpec{
				Mode:        TokenPerMessage,
				Input:       []string{"event.agentEvent.modelUsage[].inputTokens", "event.agentEvent.modelUsage[].input"},
				Output:      []string{"event.agentEvent.modelUsage[].outputTokens", "event.agentEvent.modelUsage[].output"},
				CacheRead:   []string{"event.agentEvent.modelUsage[].cacheInputTokens"},
				CacheCreate: []string{"event.agentEvent.modelUsage[].cacheCreateTokens"},
			},
			Model:    []string{"event.agentEvent.modelUsage[].model"},
			Provider: []string{"event.agentEvent.modelUsage[].provider"},
		},
		Cost: CostSpec{Rule: CostInFile, InFilePath: []string{"event.agentEvent.modelUsage[].cost"}},
	}
}

func TestEngine_JunieTokensAndInFileCost(t *testing.T) {
	r, err := parseJSONL(junieDescriptor(), filepath.Join("testdata", "junie-session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Session.TokenUsage.InputTokens != 2034 { // 1234 + 800
		t.Fatalf("want 2034 input, got %d", r.Session.TokenUsage.InputTokens)
	}
	if r.Session.Model != "claude-opus-4-6" {
		t.Fatalf("model wrong: %q", r.Session.Model)
	}
	if r.InFileCost < 0.0202 || r.InFileCost > 0.0204 { // 0.0123 + 0.0080
		t.Fatalf("want ~0.0203 cost, got %f", r.InFileCost)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/provider/ -run TestEngine -v`
Expected: FAIL — `undefined: parseJSONL`.

- [ ] **Step 3: Write the engine**

```go
// server/internal/provider/engine.go
package provider

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
	"github.com/lx-wnk/agent-dashboard/sdk"
)

// EngineResult is the parsed output of one session file: the SessionData the
// merger consumes, plus the in-file provider and cost the cost-classifier needs.
type EngineResult struct {
	Session    *parser.SessionData
	Provider   string  // in-file model provider (e.g. "ollama", "anthropic"); "" if none
	InFileCost float64 // summed in-file cost; only meaningful when Cost.Rule == CostInFile
}

// parseJSONL parses one session file per a jsonl descriptor. Unreadable lines
// are skipped. Token fields aggregate by descriptor mode; model/provider take
// the last non-empty value; in-file cost sums across matching lines.
func parseJSONL(d Descriptor, path string) (*EngineResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("provider.parseJSONL: %w", err)
	}
	defer f.Close()

	tok := d.Parse.Tokens
	var in, out, cr, cc float64
	var model, prov string
	var cost float64

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal(line, &obj); err != nil {
			continue
		}
		// Model/provider are read from any line that carries them (e.g. Codex
		// emits model on turn_context, tokens on a different line).
		if m := firstString(obj, d.Parse.Model); m != "" {
			model = m
		}
		if p := firstString(obj, d.Parse.Provider); p != "" {
			prov = p
		}
		if !matchesFilter(obj, d.Parse.EventFilter) {
			continue
		}
		in = accumulate(in, obj, tok.Input, tok.Mode)
		out = accumulate(out, obj, tok.Output, tok.Mode)
		cr = accumulate(cr, obj, tok.CacheRead, tok.Mode)
		cc = accumulate(cc, obj, tok.CacheCreate, tok.Mode)
		if d.Cost.Rule == CostInFile {
			for _, v := range resolveFirst(obj, d.Cost.InFilePath) {
				cost += toFloat(v)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("provider.parseJSONL scan: %w", err)
	}

	return &EngineResult{
		Session: &parser.SessionData{
			TokenUsage: sdk.TokenUsage{
				InputTokens:         int(in),
				OutputTokens:        int(out),
				CacheReadTokens:     int(cr),
				CacheCreationTokens: int(cc),
			},
			Model: model,
		},
		Provider:   prov,
		InFileCost: cost,
	}, nil
}

// accumulate folds the values at paths into acc per mode: cumulative keeps the
// last line's summed value; perMessage adds every line's values to the running
// total.
func accumulate(acc float64, obj map[string]any, paths []string, mode TokenMode) float64 {
	vals := resolveFirst(obj, paths)
	if len(vals) == 0 {
		return acc
	}
	var sum float64
	for _, v := range vals {
		sum += toFloat(v)
	}
	if mode == TokenCumulative {
		return sum // replace: latest line is the running total
	}
	return acc + sum
}

// matchesFilter reports whether obj passes the event filter (nil filter = all).
func matchesFilter(obj map[string]any, f *EventFilter) bool {
	if f == nil {
		return true
	}
	for _, v := range resolvePath(obj, f.Path) {
		if s, ok := v.(string); ok && s == f.Equals {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/provider/ -run TestEngine -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add server/internal/provider/engine.go server/internal/provider/engine_test.go
git commit -m "feat: add declarative JSONL parse engine for providers"
```

---

## Phase 4 — Ollama classifier

### Task 5: Ollama local-model classifier

**Files:**
- Create: `server/internal/provider/ollama.go`
- Test: `server/internal/provider/ollama_test.go`

- [ ] **Step 1: Write the failing test**

```go
// server/internal/provider/ollama_test.go
package provider

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOllama_ModelInTags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"models":[{"name":"qwen2.5-coder:7b"},{"name":"llama3.2:latest"}]}`))
	}))
	defer srv.Close()
	oc := NewOllamaClassifier(srv.URL)

	if !oc.IsLocal("ollama", "qwen2.5-coder:7b") {
		t.Fatal("expected qwen2.5-coder:7b to be local")
	}
	if !oc.IsLocal("", "ollama_chat/llama3.2:latest") { // prefix stripped
		t.Fatal("expected prefixed model to be local")
	}
	if oc.IsLocal("", "gpt-5-codex") {
		t.Fatal("expected cloud model not local")
	}
}

func TestOllama_ProviderEqualsAlwaysLocal(t *testing.T) {
	oc := NewOllamaClassifier("http://127.0.0.1:1") // unreachable
	if !oc.IsLocal("ollama", "anything") {
		t.Fatal("provider==ollama must classify local without tags")
	}
}

func TestOllama_UnreachableFallsBackToCloud(t *testing.T) {
	oc := NewOllamaClassifier("http://127.0.0.1:1") // refused
	if oc.IsLocal("", "qwen2.5-coder:7b") {
		t.Fatal("unreachable ollama must not classify by-name as local")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/provider/ -run TestOllama -v`
Expected: FAIL — `undefined: NewOllamaClassifier`.

- [ ] **Step 3: Write the classifier**

```go
// server/internal/provider/ollama.go
package provider

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// OllamaClassifier decides whether a model is a locally-served (zero-cost)
// Ollama model. It caches the installed-model set from GET <base>/api/tags for
// a short TTL; an unreachable Ollama yields an empty set (by-name match fails,
// but an explicit provider=="ollama" still classifies local).
type OllamaClassifier struct {
	base string
	http *http.Client

	mu       sync.Mutex
	tags     map[string]bool
	fetched  time.Time
	ttl      time.Duration
}

func NewOllamaClassifier(base string) *OllamaClassifier {
	return &OllamaClassifier{
		base: strings.TrimRight(base, "/"),
		http: &http.Client{Timeout: 800 * time.Millisecond},
		ttl:  10 * time.Second,
	}
}

// IsLocal reports whether (provider, model) denotes a local zero-cost model.
func (o *OllamaClassifier) IsLocal(provider, model string) bool {
	if strings.EqualFold(provider, "ollama") {
		return true
	}
	name := normalizeModel(model)
	if name == "" {
		return false
	}
	return o.tagSet()[name]
}

// normalizeModel strips known local-provider prefixes and lowercases.
func normalizeModel(m string) string {
	m = strings.ToLower(strings.TrimSpace(m))
	for _, p := range []string{"ollama_chat/", "ollama/"} {
		m = strings.TrimPrefix(m, p)
	}
	return m
}

func (o *OllamaClassifier) tagSet() map[string]bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.tags != nil && time.Since(o.fetched) < o.ttl {
		return o.tags
	}
	o.tags = o.fetchTags()
	o.fetched = time.Now()
	return o.tags
}

func (o *OllamaClassifier) fetchTags() map[string]bool {
	set := map[string]bool{}
	resp, err := o.http.Get(o.base + "/api/tags")
	if err != nil {
		return set
	}
	defer resp.Body.Close()
	var body struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if json.NewDecoder(resp.Body).Decode(&body) != nil {
		return set
	}
	for _, m := range body.Models {
		set[strings.ToLower(m.Name)] = true
	}
	return set
}
```

Note: `time.Now()`/`time.Since` are allowed in production Go (the Date/Math restriction applies only to Workflow scripts, not the codebase). The unreachable test passes because `fetchTags` returns an empty set on connection refusal.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/provider/ -run TestOllama -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add server/internal/provider/ollama.go server/internal/provider/ollama_test.go
git commit -m "feat: add Ollama local-model cost classifier"
```

---

## Phase 5 — Registry, descriptors, enable seam

### Task 6: Built-in descriptors + embed

**Files:**
- Create: `server/internal/provider/providers/codex.yaml`
- Create: `server/internal/provider/providers/gemini.yaml`
- Create: `server/internal/provider/providers/junie.yaml`
- Create: `server/internal/provider/embed.go`

- [ ] **Step 1: Write codex.yaml**

```yaml
# server/internal/provider/providers/codex.yaml
id: codex
displayName: Codex CLI
enabled: false
exeNames: [codex]
configDir:
  env: CODEX_HOME
  default: ~/.codex
sessionGlob: sessions/**/rollout-*.jsonl
source: jsonl
parse:
  eventFilter:
    path: payload.type
    equals: token_count
  tokens:
    mode: cumulative
    input: [payload.info.total_token_usage.input_tokens]
    output: [payload.info.total_token_usage.output_tokens]
    cacheRead: [payload.info.total_token_usage.cached_input_tokens]
  model: [payload.model]
  provider: [payload.model_provider]
cost:
  rule: byModel
  localIf:
    providerEquals: ollama
    orModelInOllamaTags: true
```

- [ ] **Step 2: Write gemini.yaml**

```yaml
# server/internal/provider/providers/gemini.yaml
id: gemini
displayName: Gemini CLI
enabled: false
exeNames: [gemini]
configDir:
  default: ~/.gemini
sessionGlob: tmp/*/chats/session-*.jsonl
source: jsonl
parse:
  eventFilter:
    path: type
    equals: gemini
  tokens:
    mode: perMessage
    input: [tokens.input]
    output: [tokens.output]
    cacheRead: [tokens.cached]
  model: [model]
cost:
  rule: byModel
  localIf:
    providerEquals: ollama
    orModelInOllamaTags: true
```

- [ ] **Step 3: Write junie.yaml**

```yaml
# server/internal/provider/providers/junie.yaml
id: junie
displayName: Junie CLI
enabled: false
exeNames: [junie]
configDir:
  default: ~/.junie
sessionGlob: sessions/*/events.jsonl
source: jsonl
parse:
  eventFilter:
    path: kind
    equals: LlmResponseMetadataEvent
  tokens:
    mode: perMessage
    input: [event.agentEvent.modelUsage[].inputTokens, event.agentEvent.modelUsage[].input]
    output: [event.agentEvent.modelUsage[].outputTokens, event.agentEvent.modelUsage[].output]
    cacheRead: [event.agentEvent.modelUsage[].cacheInputTokens, event.agentEvent.modelUsage[].cacheReadInputTokens]
    cacheCreate: [event.agentEvent.modelUsage[].cacheCreateTokens, event.agentEvent.modelUsage[].cacheCreationInputTokens]
  model: [event.agentEvent.modelUsage[].model]
  provider: [event.agentEvent.modelUsage[].provider]
cost:
  rule: inFile
  inFilePath: [event.agentEvent.modelUsage[].cost]
  localIf:
    providerEquals: ollama
    orModelInOllamaTags: true
```

- [ ] **Step 4: Write the embed FS**

```go
// server/internal/provider/embed.go
package provider

import "embed"

//go:embed providers/*.yaml
var builtinFS embed.FS
```

- [ ] **Step 5: Commit**

```bash
git add server/internal/provider/providers/ server/internal/provider/embed.go
git commit -m "feat: add built-in provider descriptors for codex/gemini/junie"
```

### Task 7: Enable seam + adapter stub

**Files:**
- Create: `server/internal/provider/enabled.go`
- Create: `server/internal/provider/adapter.go`

- [ ] **Step 1: Write the enable seam**

```go
// server/internal/provider/enabled.go
package provider

// EnabledFunc reports whether a provider id is enabled. Plan 2 supplies a
// DB-backed implementation; the default below uses the descriptor's own flag
// OR-ed with an explicit enabled-id allowlist (from DASHBOARD_PROVIDERS_ENABLED).
type EnabledFunc func(id string) bool

// DefaultEnabled returns an EnabledFunc: a provider is on if its descriptor sets
// enabled:true OR its id appears in allow.
func DefaultEnabled(descriptors map[string]Descriptor, allow []string) EnabledFunc {
	set := map[string]bool{}
	for _, id := range allow {
		set[id] = true
	}
	return func(id string) bool {
		if set[id] {
			return true
		}
		d, ok := descriptors[id]
		return ok && d.Enabled
	}
}
```

- [ ] **Step 2: Write the adapter stub**

```go
// server/internal/provider/adapter.go
package provider

import "github.com/lx-wnk/agent-dashboard/server/internal/parser"

// Adapter parses providers whose sessions are not file-per-session JSONL
// (IDE-embedded: Cursor, Copilot-in-VSCode, Windsurf). No adapter is registered
// in this plan; the seam exists so descriptors with source "custom:<id>" load
// without error and route here once an adapter is added.
type Adapter interface {
	// ConfigDirs returns existing config-dir paths for this provider.
	ConfigDirs() []string
	// ResolveSession returns the newest session for cwd, or (nil, false).
	ResolveSession(cwd string) (*parser.SessionData, bool)
}

var adapters = map[string]Adapter{}

// RegisterAdapter binds an adapter to a custom-source id. Unused this plan.
func RegisterAdapter(id string, a Adapter) { adapters[id] = a }
```

- [ ] **Step 3: Verify it builds**

Run: `cd server && go build ./internal/provider/`
Expected: builds clean.

- [ ] **Step 4: Commit**

```bash
git add server/internal/provider/enabled.go server/internal/provider/adapter.go
git commit -m "feat: add provider enable seam and custom-adapter stub"
```

### Task 8: Registry — load, gate, dispatch

**Files:**
- Create: `server/internal/provider/registry.go`
- Test: `server/internal/provider/registry_test.go`

The registry loads built-in + user descriptors (validating each, dropping bad ones), exposes `DetectProvider(comm)`, `ConfigDirs()` (enabled only, existing dirs), `ResolveSession(provider, cwd, claimed)`, and `Cost(provider, usage, model, inFileCost, inFileProvider)`.

- [ ] **Step 1: Write the failing test**

```go
// server/internal/provider/registry_test.go
package provider

import (
	"testing"

	"github.com/lx-wnk/agent-dashboard/sdk"
)

func testRegistry(t *testing.T, enabled ...string) *Registry {
	t.Helper()
	reg, err := NewRegistry(Options{
		UserDir:   "",
		EnabledFn: nil, // set after load below
		Ollama:    NewOllamaClassifier("http://127.0.0.1:1"),
		Pricing:   stubPricing{},
	})
	if err != nil {
		t.Fatal(err)
	}
	reg.SetEnabled(DefaultEnabled(reg.Descriptors(), enabled))
	return reg
}

// stubPricing implements the Pricing seam for cost tests.
type stubPricing struct{}

func (stubPricing) HasPricing(model string) bool { return model == "gpt-5-codex" }
func (stubPricing) EstimateCost(u sdk.TokenUsage, model string) float64 {
	return float64(u.InputTokens) / 1000
}
func (stubPricing) EstimateCacheCreationCost(sdk.TokenUsage, string) float64 { return 0 }
func (stubPricing) EstimateCacheReadCost(sdk.TokenUsage, string) float64     { return 0 }

func TestRegistry_LoadsBuiltins(t *testing.T) {
	reg := testRegistry(t)
	for _, id := range []string{"codex", "gemini", "junie"} {
		if _, ok := reg.Descriptors()[id]; !ok {
			t.Fatalf("missing built-in descriptor %q", id)
		}
	}
}

func TestRegistry_DetectProviderOnlyWhenEnabled(t *testing.T) {
	reg := testRegistry(t) // nothing enabled
	if got := reg.DetectProvider("/usr/local/bin/codex"); got != "" {
		t.Fatalf("disabled provider must not detect, got %q", got)
	}
	reg2 := testRegistry(t, "codex")
	if got := reg2.DetectProvider("codex --foo"); got != sdk.Provider("codex") {
		t.Fatalf("enabled codex should detect, got %q", got)
	}
}

func TestRegistry_CostByModelKnown(t *testing.T) {
	reg := testRegistry(t, "codex")
	cb := reg.Cost("codex", sdk.TokenUsage{InputTokens: 2000}, "gpt-5-codex", 0, "openai")
	if cb.Unknown || cb.Local {
		t.Fatalf("known cloud model should be priced, got %+v", cb)
	}
	if cb.Total != 2.0 {
		t.Fatalf("want 2.0, got %f", cb.Total)
	}
}

func TestRegistry_CostLocalWhenProviderOllama(t *testing.T) {
	reg := testRegistry(t, "codex")
	cb := reg.Cost("codex", sdk.TokenUsage{InputTokens: 9999}, "qwen2.5-coder:7b", 0, "ollama")
	if !cb.Local || cb.Total != 0 {
		t.Fatalf("ollama-provider session should be local $0, got %+v", cb)
	}
}

func TestRegistry_CostInFileJunie(t *testing.T) {
	reg := testRegistry(t, "junie")
	cb := reg.Cost("junie", sdk.TokenUsage{InputTokens: 100}, "claude-opus-4-6", 0.0203, "anthropic")
	if cb.Unknown || cb.Local || cb.Total != 0.0203 {
		t.Fatalf("junie in-file cost should pass through, got %+v", cb)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/provider/ -run TestRegistry -v`
Expected: FAIL — `undefined: NewRegistry`.

- [ ] **Step 3: Write the registry**

```go
// server/internal/provider/registry.go
package provider

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
	"github.com/lx-wnk/agent-dashboard/sdk"
	"gopkg.in/yaml.v3"
)

// Pricing is the seam to the pricing table (parser/pricing wrappers). Defined
// as an interface so the registry stays testable without the real table.
type Pricing interface {
	HasPricing(model string) bool
	EstimateCost(u sdk.TokenUsage, model string) float64
	EstimateCacheCreationCost(u sdk.TokenUsage, model string) float64
	EstimateCacheReadCost(u sdk.TokenUsage, model string) float64
}

// CostBreakdown mirrors merger.CostBreakdown plus a Local flag for $0 local models.
type CostBreakdown struct {
	Total       float64
	CacheCreate float64
	CacheRead   float64
	Unknown     bool
	Local       bool
}

type Options struct {
	UserDir   string
	EnabledFn EnabledFunc
	Ollama    *OllamaClassifier
	Pricing   Pricing
}

type Registry struct {
	descriptors map[string]Descriptor
	byExe       map[string]string // exe base name -> provider id
	enabled     EnabledFunc
	ollama      *OllamaClassifier
	pricing     Pricing
}

func NewRegistry(opt Options) (*Registry, error) {
	r := &Registry{
		descriptors: map[string]Descriptor{},
		byExe:       map[string]string{},
		enabled:     opt.EnabledFn,
		ollama:      opt.Ollama,
		pricing:     opt.Pricing,
	}
	if r.enabled == nil {
		r.enabled = func(string) bool { return false }
	}
	if err := r.loadFS(builtinFS, "providers"); err != nil {
		return nil, err
	}
	if opt.UserDir != "" {
		if entries, err := os.ReadDir(opt.UserDir); err == nil {
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
					continue
				}
				r.loadFile(filepath.Join(opt.UserDir, e.Name()))
			}
		}
	}
	return r, nil
}

func (r *Registry) loadFS(fsys fs.FS, dir string) error {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return fmt.Errorf("provider.loadFS: %w", err)
	}
	for _, e := range entries {
		b, err := fs.ReadFile(fsys, filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		r.ingest(b, e.Name())
	}
	return nil
}

func (r *Registry) loadFile(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	r.ingest(b, path)
}

// ingest decodes, validates, and registers one descriptor. A bad descriptor is
// logged and dropped — never fatal (cf. the channel-bridge panic lesson).
func (r *Registry) ingest(b []byte, name string) {
	var d Descriptor
	if err := yaml.Unmarshal(b, &d); err != nil {
		slog.Warn("provider descriptor parse failed", "file", name, "err", err)
		return
	}
	if err := d.Validate(); err != nil {
		slog.Warn("provider descriptor invalid", "file", name, "err", err)
		return
	}
	r.descriptors[d.ID] = d
	for _, exe := range d.ExeNames {
		r.byExe[exe] = d.ID
	}
}

func (r *Registry) Descriptors() map[string]Descriptor { return r.descriptors }
func (r *Registry) SetEnabled(fn EnabledFunc)          { r.enabled = fn }

// DetectProvider maps a process command to an enabled provider id, or "".
func (r *Registry) DetectProvider(comm string) sdk.Provider {
	comm = strings.TrimSpace(comm)
	if comm == "" {
		return ""
	}
	if i := strings.IndexByte(comm, ' '); i >= 0 {
		comm = comm[:i]
	}
	base := filepath.Base(comm)
	if base == "claude" {
		return sdk.ProviderClaude // Claude stays built-in, always on
	}
	id, ok := r.byExe[base]
	if !ok || !r.enabled(id) {
		return ""
	}
	return sdk.Provider(id)
}

// ConfigDirs returns existing config dirs for all enabled jsonl providers.
func (r *Registry) ConfigDirs() []parser.ProviderConfigDir {
	home, _ := os.UserHomeDir()
	var out []parser.ProviderConfigDir
	ids := make([]string, 0, len(r.descriptors))
	for id := range r.descriptors {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		d := r.descriptors[id]
		if !r.enabled(id) || d.IsCustom() {
			continue
		}
		dir := expandHome(d.ConfigDir.Default, home)
		if d.ConfigDir.Env != "" {
			if v := os.Getenv(d.ConfigDir.Env); v != "" {
				dir = v
			}
		}
		if dir != "" && dirExists(dir) {
			out = append(out, parser.ProviderConfigDir{Provider: sdk.Provider(id), Path: dir})
		}
	}
	return out
}

// ResolveSession finds and parses the newest session file for a non-Claude
// provider under cwd. claimed excludes already-bound session ids.
func (r *Registry) ResolveSession(p sdk.Provider, cwd string, claimed map[string]bool) (*parser.SessionData, string, float64, error) {
	d, ok := r.descriptors[string(p)]
	if !ok || d.IsCustom() {
		return nil, "", 0, fmt.Errorf("no jsonl descriptor for %s", p)
	}
	encoded := parser.EncodePath(cwd)
	_ = encoded // most providers glob relative to configDir, not the encoded path
	for _, pcd := range r.ConfigDirs() {
		if pcd.Provider != p {
			continue
		}
		matches, _ := filepath.Glob(filepath.Join(pcd.Path, filepath.FromSlash(d.SessionGlob)))
		sort.Slice(matches, func(i, j int) bool {
			return fileMtime(matches[i]).After(fileMtime(matches[j]))
		})
		for _, path := range matches {
			id := strings.TrimSuffix(filepath.Base(path), ".jsonl")
			if claimed != nil && claimed[id] {
				continue
			}
			res, err := parseJSONL(d, path)
			if err != nil {
				continue
			}
			res.Session.SessionID = id
			res.Session.ProjectPath = cwd
			res.Session.Path = path
			if claimed != nil {
				claimed[id] = true
			}
			return res.Session, res.Provider, res.InFileCost, nil
		}
	}
	return nil, "", 0, fmt.Errorf("no %s session for %s", p, cwd)
}

// Cost computes the cost breakdown for a provider session, honoring localIf
// ($0), in-file cost, and the pricing table.
func (r *Registry) Cost(p sdk.Provider, usage sdk.TokenUsage, model string, inFileCost float64, inFileProvider string) CostBreakdown {
	d, ok := r.descriptors[string(p)]
	if !ok {
		// Claude or unknown: fall through to pricing table.
		return CostBreakdown{
			Total:       r.pricing.EstimateCost(usage, model),
			CacheCreate: r.pricing.EstimateCacheCreationCost(usage, model),
			CacheRead:   r.pricing.EstimateCacheReadCost(usage, model),
		}
	}
	if d.Cost.LocalIf != nil && r.ollama != nil {
		li := d.Cost.LocalIf
		if r.ollama.IsLocal(inFileProvider, model) ||
			(li.ProviderEquals != "" && strings.EqualFold(inFileProvider, li.ProviderEquals)) {
			return CostBreakdown{Local: true}
		}
	}
	switch d.Cost.Rule {
	case CostInFile:
		return CostBreakdown{Total: inFileCost}
	case CostNone:
		return CostBreakdown{Unknown: true}
	default: // byModel
		if !r.pricing.HasPricing(model) {
			return CostBreakdown{Unknown: true}
		}
		return CostBreakdown{
			Total:       r.pricing.EstimateCost(usage, model),
			CacheCreate: r.pricing.EstimateCacheCreationCost(usage, model),
			CacheRead:   r.pricing.EstimateCacheReadCost(usage, model),
		}
	}
}

func expandHome(p, home string) string {
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func fileMtime(p string) (t timeTime) {
	if info, err := os.Stat(p); err == nil {
		return info.ModTime()
	}
	return
}
```

Add at the top of `registry.go` (with the imports) a small alias so the helper reads cleanly:

```go
import "time"

type timeTime = time.Time
```

(Or inline `time.Time` directly in `fileMtime` — the alias is only to keep the helper signature short. Use whichever the reviewer prefers; inline `time.Time` is fine.)

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/provider/ -run TestRegistry -v`
Expected: PASS (5 tests). If `EncodePath`/`dirExists` collide with parser exports, drop the unused `encoded` line and the local `dirExists` if already provided — keep one definition.

- [ ] **Step 5: Run the whole provider package**

Run: `cd server && go test ./internal/provider/ -v`
Expected: PASS (all tasks' tests).

- [ ] **Step 6: Commit**

```bash
git add server/internal/provider/registry.go server/internal/provider/registry_test.go
git commit -m "feat: add provider registry with detection, resolve and cost"
```

---

## Phase 6 — Wire the registry into the seams

### Task 9: sdk — add Junie const + CostLocal field

**Files:**
- Modify: `sdk/types.go:82-89` (consts), `sdk/types.go` Agent struct (add field near `CostUnknown`).

- [ ] **Step 1: Add the provider const**

Change (lines 82-89):
```go
const (
	ProviderClaude Provider = "claude"
	ProviderCodex  Provider = "codex"
	ProviderGemini Provider = "gemini"
)
```
to:
```go
const (
	ProviderClaude Provider = "claude"
	ProviderCodex  Provider = "codex"
	ProviderGemini Provider = "gemini"
	ProviderJunie  Provider = "junie"
)
```

- [ ] **Step 2: Add CostLocal to Agent**

Immediately after the `CostUnknown bool ...` field in the `Agent` struct, add:
```go
	// CostLocal is true when the session runs a locally-served (Ollama) model,
	// whose cost is $0 rather than unknown. CostEstimate is 0 in this case.
	CostLocal bool `json:"costLocal,omitempty"`
```

- [ ] **Step 3: Verify build + regenerate the SDK TS types if applicable**

Run: `cd server && go build ./... && cd .. && task` (or the SDK-gen task; the project lints `sdk.generated.ts`).
Expected: builds; the generated TS includes `costLocal`.

- [ ] **Step 4: Commit**

```bash
git add sdk/types.go
git commit -m "feat: add Junie provider and CostLocal agent field"
```

### Task 10: Pricing adapter in provider wiring

**Files:**
- Create: `server/internal/merger/pricing_adapter.go` (a thin type implementing `provider.Pricing` over the existing `parser` wrappers)

The registry needs a `provider.Pricing`. The real pricing wrappers live in `parser` (`HasPricing`, `EstimateCost`, …). Wrap them.

- [ ] **Step 1: Write the adapter**

```go
// server/internal/merger/pricing_adapter.go
package merger

import (
	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
	"github.com/lx-wnk/agent-dashboard/sdk"
)

// pricingAdapter exposes the parser pricing wrappers as a provider.Pricing.
type pricingAdapter struct{}

func (pricingAdapter) HasPricing(model string) bool { return parser.HasPricing(model) }
func (pricingAdapter) EstimateCost(u sdk.TokenUsage, model string) float64 {
	return parser.EstimateCost(u, model)
}
func (pricingAdapter) EstimateCacheCreationCost(u sdk.TokenUsage, model string) float64 {
	return parser.EstimateCacheCreationCost(u, model)
}
func (pricingAdapter) EstimateCacheReadCost(u sdk.TokenUsage, model string) float64 {
	return parser.EstimateCacheReadCost(u, model)
}
```

- [ ] **Step 2: Verify build**

Run: `cd server && go build ./internal/merger/`
Expected: builds (the adapter is unused until Task 12 — acceptable; Go allows unused package-level types).

- [ ] **Step 3: Commit**

```bash
git add server/internal/merger/pricing_adapter.go
git commit -m "feat: add pricing adapter bridging parser pricing to provider.Pricing"
```

### Task 11: scanner — delegate provider detection to the registry

**Files:**
- Modify: `server/internal/scanner/scanner.go` (`DetectProviderFromCommand` + add a registry field to the scanner)

The scanner currently calls package-level `DetectProviderFromCommand`. Add a `ProviderDetector` seam so the scanner asks the registry; keep the old function as a Claude-only fallback for callers that have no registry.

- [ ] **Step 1: Write the failing test**

```go
// server/internal/scanner/scanner_provider_test.go
package scanner

import (
	"testing"

	"github.com/lx-wnk/agent-dashboard/sdk"
)

type fakeDetector struct{}

func (fakeDetector) DetectProvider(comm string) sdk.Provider {
	if comm == "codex" {
		return sdk.ProviderCodex
	}
	return ""
}

func TestDetectVia_UsesInjectedDetector(t *testing.T) {
	got := detectProviderVia(fakeDetector{}, "codex")
	if got != sdk.ProviderCodex {
		t.Fatalf("want codex, got %q", got)
	}
	// claude always resolves even when detector returns "" for it
	if detectProviderVia(fakeDetector{}, "claude") != sdk.ProviderClaude {
		t.Fatal("claude must always resolve")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && go test ./internal/scanner/ -run TestDetectVia -v`
Expected: FAIL — `undefined: detectProviderVia`.

- [ ] **Step 3: Add the seam**

Add to `scanner.go`:
```go
// ProviderDetector maps a process command to a provider. The provider Registry
// implements this; tests can fake it.
type ProviderDetector interface {
	DetectProvider(comm string) sdk.Provider
}

// detectProviderVia resolves a process command through an injected detector,
// always honoring Claude even when no detector or a disabled detector is given.
func detectProviderVia(d ProviderDetector, comm string) sdk.Provider {
	if d != nil {
		if p := d.DetectProvider(comm); p != "" {
			return p
		}
	}
	if base := commBase(comm); base == "claude" {
		return sdk.ProviderClaude
	}
	return ""
}

// commBase extracts argv[0]'s base name from a command string.
func commBase(comm string) string {
	comm = strings.TrimSpace(comm)
	if i := strings.IndexByte(comm, ' '); i >= 0 {
		comm = comm[:i]
	}
	return filepath.Base(comm)
}
```

Keep the existing `DetectProviderFromCommand` for now (Claude + the legacy switch) so nothing else breaks; the scan loop is migrated to `detectProviderVia` where the scanner has a detector reference. (If the scanner is a struct, add a `detector ProviderDetector` field set in its constructor and replace its `DetectProviderFromCommand(cmd)` call with `detectProviderVia(s.detector, cmd)`. Locate the call site with: `grep -n DetectProviderFromCommand server/internal/scanner/scanner.go`.)

- [ ] **Step 4: Run to verify it passes**

Run: `cd server && go test ./internal/scanner/ -run TestDetectVia -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/scanner/scanner.go server/internal/scanner/scanner_provider_test.go
git commit -m "feat: add injectable provider detector seam to scanner"
```

### Task 12: merger — resolve non-Claude sessions + cost via the registry

**Files:**
- Modify: `server/internal/merger/merger.go` (`EstimateCostForProvider` call site in `buildAgent`; non-Claude resolve path)
- Modify: `server/internal/parser/resolver.go` — remove `resolverFor`/`ResolveNonClaudeSession`/`providerConfigDirs` (moved into the registry). Keep `listJSONLByMtime`/`sessionFileCandidate` only if still referenced elsewhere (`grep -rn listJSONLByMtime server/`); otherwise delete the file.
- Modify: `server/internal/parser/parser.go` — `AllAgentConfigDirs` drops Codex/Gemini stubs.

This task assumes the merger is a struct (per the recent "merger struct" refactor). Add a `registry *provider.Registry` field.

- [ ] **Step 1: Make AllAgentConfigDirs Claude-only**

In `parser.go`, replace the Codex and Gemini blocks in `AllAgentConfigDirs` (lines ~175-200) so only Claude dirs remain:
```go
func AllAgentConfigDirs() []ProviderConfigDir {
	var result []ProviderConfigDir
	for _, d := range allClaudeConfigDirs() {
		result = append(result, ProviderConfigDir{Provider: sdk.ProviderClaude, Path: d})
	}
	return result
}
```
The enabled non-Claude config dirs now come from `registry.ConfigDirs()`. Where the scan composes the full dir list, concatenate `parser.AllAgentConfigDirs()` with `registry.ConfigDirs()`. Find that composition: `grep -rn AllAgentConfigDirs server/internal/`.

- [ ] **Step 2: Route cost through the registry in buildAgent**

In `merger.go` `buildAgent`, the current call is:
```go
c := EstimateCostForProvider(provider, session.TokenUsage, session.Model)
```
The merger struct must carry the registry and the in-file provider/cost from the resolve step. Change the resolve path so non-Claude sessions are resolved via `m.registry.ResolveSession(...)`, capturing `inFileProvider` and `inFileCost`, then:
```go
var c CostBreakdown
if provider == sdk.ProviderClaude || m.registry == nil {
	c = EstimateCostForProvider(provider, session.TokenUsage, session.Model)
} else {
	rc := m.registry.Cost(provider, session.TokenUsage, session.Model, inFileCost, inFileProvider)
	c = CostBreakdown{Total: rc.Total, CacheCreate: rc.CacheCreate, CacheRead: rc.CacheRead, Unknown: rc.Unknown}
	agentCostLocal = rc.Local
}
```
Add the `CostLocal: agentCostLocal,` field to the returned `sdk.Agent` literal (next to `CostUnknown: c.Unknown,`).

> Threading note: `inFileProvider`, `inFileCost`, and `agentCostLocal` must be passed into `buildAgent` (extend its signature) or computed in the caller that already holds the resolve result. Prefer extending `buildAgent(proc, session, baselineCost, inFileProvider string, inFileCost float64)` and computing `CostLocal` inside. Update all `buildAgent` call sites — find them: `grep -rn 'buildAgent(' server/internal/merger/`.

- [ ] **Step 3: Replace the non-Claude resolve call**

Wherever `parser.ResolveNonClaudeSession(...)` is called (find: `grep -rn ResolveNonClaudeSession server/internal/`), replace with:
```go
session, inFileProvider, inFileCost, err := m.registry.ResolveSession(proc.Provider, proc.CWD, claimed)
```
Then delete `ResolveNonClaudeSession`, `resolverFor`, and `providerConfigDirs` from `parser/resolver.go`.

- [ ] **Step 4: Build the whole server**

Run: `cd server && go build ./...`
Expected: builds. Fix any remaining references to the deleted parser functions (the compiler lists them).

- [ ] **Step 5: Run merger + parser tests**

Run: `cd server && go test ./internal/merger/ ./internal/parser/ -v`
Expected: PASS. Update any test that asserted Codex/Gemini appear in `AllAgentConfigDirs` — those now come from the registry (move/adjust the assertion).

- [ ] **Step 6: Commit**

```bash
git add server/internal/merger/ server/internal/parser/
git commit -m "feat: resolve non-Claude sessions and cost via provider registry"
```

### Task 13: config — provider dir + enabled list

**Files:**
- Modify: `server/internal/config/config.go` (Config struct + Defaults if needed)

- [ ] **Step 1: Add fields**

In the `Config` struct add:
```go
	// ProviderDir is an optional directory of user provider descriptors merged
	// over the built-ins. Set via DASHBOARD_PROVIDER_DIR.
	ProviderDir string `koanf:"provider_dir"`
	// ProvidersEnabled is the explicit allowlist of enabled provider ids until
	// the DB-backed Settings UI (Plan 2) lands. Set via
	// DASHBOARD_PROVIDERS_ENABLED as a comma list, e.g. "codex,junie".
	ProvidersEnabled []string `koanf:"providers_enabled"`
```

- [ ] **Step 2: Verify it loads**

Run: `cd server && go build ./internal/config/ && go test ./internal/config/ -v`
Expected: builds; existing config tests pass. (koanf maps `DASHBOARD_PROVIDER_DIR`→`provider_dir`, comma env→slice per koanf's default behavior; if the slice needs a delimiter, add a small split in `Load` mirroring any existing slice field — `grep -n '\[\]string' server/internal/config/config.go`.)

- [ ] **Step 3: Commit**

```bash
git add server/internal/config/config.go
git commit -m "feat: add provider dir and enabled-list config"
```

### Task 14: di.go — construct and wire the registry

**Files:**
- Modify: `cmd/serve/di.go`

- [ ] **Step 1: Construct the registry**

In the DI assembly (where the merger and scanner are built), add:
```go
ollama := provider.NewOllamaClassifier("http://localhost:11434")
reg, err := provider.NewRegistry(provider.Options{
	UserDir: cfg.ProviderDir,
	Ollama:  ollama,
	Pricing: merger.PricingAdapter(), // exported accessor for pricingAdapter{}
})
if err != nil {
	return nil, fmt.Errorf("provider registry: %w", err)
}
reg.SetEnabled(provider.DefaultEnabled(reg.Descriptors(), cfg.ProvidersEnabled))
```
Add an exported accessor in `merger` (since `pricingAdapter` is unexported):
```go
// merger/pricing_adapter.go
func PricingAdapter() provider.Pricing { return pricingAdapter{} }
```
(Import `provider` in merger for the return type. Verify no import cycle: `provider` imports `parser` only, `merger` imports both — OK.)

- [ ] **Step 2: Pass the registry into the merger + scanner**

Wherever the merger struct and scanner are constructed, pass `reg` (extend their constructors with a `registry *provider.Registry` / `detector provider.ProviderDetector` parameter; `*provider.Registry` satisfies `ProviderDetector`). Find: `grep -rn 'merger.New\|scanner.New' cmd/serve/`.

- [ ] **Step 3: Build + run the full test suite**

Run: `cd server && go build ./... && go test ./...`
Expected: builds; all tests pass.

- [ ] **Step 4: Commit**

```bash
git add cmd/serve/di.go server/internal/merger/pricing_adapter.go
git commit -m "feat: wire provider registry into merger and scanner at startup"
```

---

## Phase 7 — Verify & document

### Task 15: End-to-end smoke + docs

**Files:**
- Modify: `README.md`, `CHANGELOG.md`, `CONTRIBUTING.md`, `PRIVACY.md`
- Modify: `.agent-context/decisions.json`, `.agent-context/memory/log.md`

- [ ] **Step 1: Full backend verification**

Run:
```bash
cd server && go build ./... && go test ./... && go vet ./...
cd .. && pnpm lint && pnpm typecheck && pnpm test
```
Expected: all green. The frontend lint/typecheck covers the regenerated `sdk.generated.ts` carrying `costLocal`.

- [ ] **Step 2: Manual smoke (optional, if a provider CLI is installed)**

If `~/.junie/sessions/*/events.jsonl` exists, set `DASHBOARD_PROVIDERS_ENABLED=junie`, run the server, and confirm a Junie agent surfaces with non-zero tokens and a real cost. If none installed, skip — the fixture tests cover parsing.

- [ ] **Step 3: Update docs**

- `README.md`: add a "Supported providers" note — Claude (always on); Codex, Gemini, Junie CLIs (opt-in via `DASHBOARD_PROVIDERS_ENABLED` until the Settings UI ships); Ollama-backed models shown as `$0`.
- `CHANGELOG.md` (Keep a Changelog): under `### Added` — "Provider registry with opt-in Codex/Gemini/Junie CLI monitoring and Ollama `$0` cost classification."
- `CONTRIBUTING.md`: note how to add a provider — drop a YAML descriptor in `server/internal/provider/providers/` (or `DASHBOARD_PROVIDER_DIR`), no code change for JSONL providers.
- `PRIVACY.md`: list the new local paths read when enabled (`~/.codex`, `~/.gemini`, `~/.junie`) and the local `localhost:11434/api/tags` poll.

- [ ] **Step 4: Update agent-context memory**

- `.agent-context/decisions.json`: append an ADR — "Provider integration via declarative descriptors + registry; dispatch moved out of parser to break the import cycle; enable-state via injected seam (DB-backed in Plan 2); IDE providers deferred to custom adapters."
- `.agent-context/memory/log.md`: one line — provider registry backend shipped (date 2026-06-22).

- [ ] **Step 5: Commit**

```bash
git add README.md CHANGELOG.md CONTRIBUTING.md PRIVACY.md .agent-context/
git commit -m "docs: document provider registry, opt-in providers, and Ollama cost"
```

---

## Self-Review Notes (resolved during authoring)

- **Spec coverage:** registry (§3) → Tasks 6-8,14; descriptor schema (§4) → Tasks 1,6; opt-in seam (§5, DB deferred to Plan 2) → Tasks 7,13; Ollama `$0` (§6, `CostLocal`) → Tasks 5,9,12; 3 declarative providers (§7) → Tasks 3,6; error handling (§8: bad descriptor dropped, path fallbacks, ollama-unreachable, exec-mode) → Tasks 8 (`ingest` logs+drops), 2 (`resolveFirst`), 5 (empty set), engine (missing token lines → 0/Unknown); testing (§9) → fixtures Task 3, table-driven throughout.
- **Out of scope, by design:** Copilot CLI (T2 stretch — not in these tasks; add a `copilot.yaml` descriptor later, identical mechanism), all IDE adapters, the `provider_setting` DB table + REST + Vue (Plan 2). The `EnabledFunc` and `Adapter` seams are the contracts Plan 2 / future adapters bind to.
- **Type consistency:** `CostBreakdown` exists in both `merger` (existing, no `Local`) and `provider` (new, with `Local`); Task 12 maps between them explicitly — intentional, not a clash. `Descriptors()` returns `map[string]Descriptor` used consistently by `DefaultEnabled`. `ProviderConfigDir` is reused from `parser` (registry returns the parser type) to avoid a parallel type.
- **Known follow-ups flagged for the executor:** the exact `buildAgent`/merger constructor signatures depend on the post-"merger struct" shape — Task 12/14 give `grep` commands to locate call sites rather than guessing line numbers. Verify the merger is a struct before Task 12; if it is still package-level funcs, thread the registry as an explicit parameter instead.
