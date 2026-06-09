# Stage-Output Write Tool — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let stage agents submit their structured per-stage result via an MCP tool (`set_stage_output`) that validates synchronously and persists to `stage_runs.output`, replacing the fragile ` ```json `-block scraping as the primary capture path.

**Architecture:** A new channel-bridge MCP tool POSTs the result to a new bearer-authenticated loopback endpoint `POST /api/channel-stage-output`. The endpoint validates the payload with the existing `pipeline.ValidateStageOutput`, then writes it to the `stage_runs.output` column. On the next completion check, `DetectCompletion` finds the populated column and uses it directly — skipping the JSONL scrape. If the column is empty (agent didn't call the tool, or a non-Claude adapter), the existing scrape-and-validate path runs unchanged. Strictly additive; no migration (the `output` JSON column already exists).

**Tech Stack:** Go 1.26 (chi, ent ORM), TypeScript (MCP stdio bridge `@modelcontextprotocol/sdk`), Go `testify`, `httptest`. Build/test via `task`.

**Spec:** `docs/superpowers/specs/2026-06-08-stage-output-write-tool-design.md` (§6 Origin-fix is deferred to a separate plan).

**Scope note:** The Origin-403 fix for the *existing* permission-create endpoints (§6 of the spec) is a security-sensitive auth change and is **out of scope here** — it gets its own plan. This plan's new endpoint is bearer-authenticated correctly from the start and introduces the `validateStageRunToken` helper that the §6 plan will reuse.

---

## File Structure

| File | Responsibility | Action |
|---|---|---|
| `server/internal/api/agents/channel_stage_output.go` | New handler `ChannelStageOutputHandler` + `validateStageRunToken` helper | Create |
| `server/internal/api/agents/channel_stage_output_test.go` | Handler unit tests (httptest + fake repo + temp discovery file) | Create |
| `server/internal/api/router.go` | Add `ChannelStageOutput` field to `RouterDeps`; mount route outside JWT group | Modify |
| `server/cmd/serve/di.go` | Construct the handler, inject into `RouterDeps` | Modify |
| `server/internal/pipeline/completion_detector.go` | Early-return when `sr.Output` is a tool-written result | Modify |
| `server/internal/pipeline/completion_detector_test.go` | Test the tool-output early-return branch | Create or Modify |
| `server/internal/pipeline/stage_prompts.go` | Switch the 3 stage output contracts to instruct calling `set_stage_output` (json-block kept as fallback) | Modify |
| `channel/dashboard-channel.ts` | Add `set_stage_output` tool (ListTools entry + handler) | Modify |
| `.agent-context/task-pipeline.md` | Add `ValidateStageOutput` to the api→pipeline runtime import whitelist | Modify |

**Reused, not modified:** `bearerToken` / `validateChannelToken` (same `agents` package, `channelreply.go:133-163`), `repo.StageRunRepo` (`GetByID`, `Update`), `pipeline.ValidateStageOutput` (`completion_detector.go:17`), `repo.UpdateStageRunInput.Output`.

---

## Task 1: Channel-stage-output handler + token helper

**Files:**
- Create: `server/internal/api/agents/channel_stage_output.go`
- Test: `server/internal/api/agents/channel_stage_output_test.go`

Auth model (mirrors `channel-reply`): the bridge sends `Authorization: Bearer <TOKEN>` where `<TOKEN>` is the value in the per-PID discovery file. We resolve `stageRunId → stage_run.pid → discovery-file token` and constant-time compare. Validation reuses `pipeline.ValidateStageOutput` (no logic duplicated). On schema failure we return `422` with the validation message so the live agent self-corrects and re-calls.

- [ ] **Step 1: Write the failing test**

```go
package agents_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/api/agents"
	"github.com/lx-wnk/agent-dashboard/server/internal/channelconfig"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/stretchr/testify/require"
)

// fakeStageRunRepo implements just enough of repo.StageRunRepo for the handler.
type fakeStageRunRepo struct {
	repo.StageRunRepo // embed so unused methods satisfy the interface (nil-panic if called)
	get               func(id string) (*ent.StageRun, error)
	updated           map[string]any
}

func (f *fakeStageRunRepo) GetByID(_ context.Context, id string) (*ent.StageRun, error) {
	return f.get(id)
}

func (f *fakeStageRunRepo) Update(_ context.Context, _ string, in repo.UpdateStageRunInput) (*ent.StageRun, error) {
	f.updated = in.Output
	return &ent.StageRun{}, nil
}

// writeDiscovery writes a per-PID discovery file containing token, returns cleanup.
func writeDiscovery(t *testing.T, pid int, token string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, channelconfig.DiscoveryDir)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	data, _ := json.Marshal(map[string]string{"token": token})
	require.NoError(t, os.WriteFile(filepath.Join(dir, jsonName(pid)), data, 0o600))
}

func jsonName(pid int) string { return itoa(pid) + ".json" }
func itoa(n int) string       { return string(rune('0'+n)) /* replaced in Step 3 */ }

func newReq(t *testing.T, token string, body any) *http.Request {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/channel-stage-output", bytes.NewReader(b))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

func TestChannelStageOutput_ValidImplementation_Persists(t *testing.T) {
	const pid = 4242
	writeDiscovery(t, pid, "tok-abc")
	p := pid
	r := &fakeStageRunRepo{get: func(id string) (*ent.StageRun, error) {
		return &ent.StageRun{ID: id, Stage: "implementation", Pid: &p}, nil
	}}
	h := agents.NewChannelStageOutputHandler(r)

	rec := httptest.NewRecorder()
	h.Post(rec, newReq(t, "tok-abc", map[string]any{
		"stageRunId": "sr-1",
		"output":     map[string]any{"summary": "did it", "commits": []string{"abc"}, "openItems": []string{}},
	}))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "did it", r.updated["summary"])
}

func TestChannelStageOutput_BadToken_401(t *testing.T) {
	const pid = 4242
	writeDiscovery(t, pid, "tok-abc")
	p := pid
	r := &fakeStageRunRepo{get: func(id string) (*ent.StageRun, error) {
		return &ent.StageRun{ID: id, Stage: "implementation", Pid: &p}, nil
	}}
	h := agents.NewChannelStageOutputHandler(r)

	rec := httptest.NewRecorder()
	h.Post(rec, newReq(t, "wrong", map[string]any{
		"stageRunId": "sr-1", "output": map[string]any{"summary": "x"},
	}))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Nil(t, r.updated)
}

func TestChannelStageOutput_SchemaInvalid_422(t *testing.T) {
	const pid = 4242
	writeDiscovery(t, pid, "tok-abc")
	p := pid
	r := &fakeStageRunRepo{get: func(id string) (*ent.StageRun, error) {
		return &ent.StageRun{ID: id, Stage: "self_review", Pid: &p}, nil
	}}
	h := agents.NewChannelStageOutputHandler(r)

	rec := httptest.NewRecorder()
	// self_review requires passed/findings/summary — send an empty object.
	h.Post(rec, newReq(t, "tok-abc", map[string]any{
		"stageRunId": "sr-1", "output": map[string]any{},
	}))
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Nil(t, r.updated)
}

func TestChannelStageOutput_UnknownStageRun_404(t *testing.T) {
	r := &fakeStageRunRepo{get: func(id string) (*ent.StageRun, error) {
		return nil, ent.NotLoadedError{}
	}}
	h := agents.NewChannelStageOutputHandler(r)
	rec := httptest.NewRecorder()
	h.Post(rec, newReq(t, "tok-abc", map[string]any{
		"stageRunId": "missing", "output": map[string]any{"summary": "x"},
	}))
	require.Equal(t, http.StatusNotFound, rec.Code)
}
```

> Note: `itoa`/`jsonName` above are throwaway stubs so the file references compile while you write the test; Step 3 replaces `itoa` with `strconv.Itoa`. Keep the test focused — these helpers exist only to build the discovery filename.

- [ ] **Step 2: Run test to verify it fails (does not compile — handler absent)**

Run: `cd server && go test ./internal/api/agents/ -run TestChannelStageOutput -v`
Expected: FAIL — `undefined: agents.NewChannelStageOutputHandler`.

- [ ] **Step 3: Fix the throwaway helper, then implement the handler**

First replace the throwaway `itoa` in the test file with the standard library — change the helper block to:

```go
func jsonName(pid int) string { return strconv.Itoa(pid) + ".json" }
```

and add `"strconv"` to the test imports, removing the old `itoa`/`jsonName` stub lines.

Now create `server/internal/api/agents/channel_stage_output.go`:

```go
package agents

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
)

// ChannelStageOutputHandler serves POST /api/channel-stage-output. The channel
// bridge posts an agent's structured stage result here. Auth: bearer token
// validated against the discovery file of the stage_run's PID. Output is
// validated against the per-stage schema before it is persisted, so a live
// agent receives a 422 and can correct without a kill-restart.
type ChannelStageOutputHandler struct {
	stageRuns repo.StageRunRepo
}

func NewChannelStageOutputHandler(stageRuns repo.StageRunRepo) *ChannelStageOutputHandler {
	return &ChannelStageOutputHandler{stageRuns: stageRuns}
}

func (h *ChannelStageOutputHandler) Post(w http.ResponseWriter, r *http.Request) {
	var body struct {
		StageRunID string         `json:"stageRunId"`
		Output     map[string]any `json:"output"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	if body.StageRunID == "" || len(body.Output) == 0 {
		http.Error(w, `{"error":"missing stageRunId or output"}`, http.StatusBadRequest)
		return
	}

	sr, err := h.stageRuns.GetByID(r.Context(), body.StageRunID)
	if err != nil || sr == nil {
		http.Error(w, `{"error":"stage_run not found"}`, http.StatusNotFound)
		return
	}

	pid := 0
	if sr.Pid != nil {
		pid = *sr.Pid
	}
	if !validateChannelToken(pid, bearerToken(r)) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	if v := pipeline.ValidateStageOutput(sr.Stage, body.Output); !v.OK {
		writeJSONError(w, http.StatusUnprocessableEntity, v.Error)
		return
	}

	if _, err := h.stageRuns.Update(r.Context(), sr.ID, repo.UpdateStageRunInput{Output: body.Output}); err != nil {
		http.Error(w, `{"error":"failed to persist output"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// ensure ent import is used even if Pid handling changes during review.
var _ = errors.Is
var _ ent.StageRun
```

> Drop the two trailing `var _` lines if `goimports`/lint flags them — they are only there to keep the `ent`/`errors` imports honest while you iterate. Final version should import exactly what it uses: remove `errors` and `ent` from the import block if unused. Run `task fmt` after.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd server && go test ./internal/api/agents/ -run TestChannelStageOutput -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Lint + vet**

Run: `cd server && go vet ./internal/api/agents/ && task lint`
Expected: no errors. Fix any unused-import findings flagged above.

- [ ] **Step 6: Commit**

```bash
git add server/internal/api/agents/channel_stage_output.go server/internal/api/agents/channel_stage_output_test.go
git commit -m "feat(pipeline): channel-stage-output endpoint with synchronous schema validation"
```

---

## Task 2: Wire the handler into the router

**Files:**
- Modify: `server/internal/api/router.go:84` (RouterDeps struct), `:359` (mount block)
- Modify: `server/cmd/serve/di.go:261` (RouterDeps construction)

The route mounts **outside** the JWT/same-origin/loopback `r.Group` — directly alongside `/api/channel-reply` (router.go:357-362) — because it is a bearer-authenticated server-to-server call carrying no `Origin` header and no JWT cookie.

- [ ] **Step 1: Add the field to RouterDeps**

In `server/internal/api/router.go`, in the `RouterDeps` struct (near line 120, next to `ChannelReply`):

```go
	ChannelReply          *agents.ChannelReplyHandler
	ChannelStageOutput    *agents.ChannelStageOutputHandler
```

- [ ] **Step 2: Mount the route next to channel-reply**

In `server/internal/api/router.go`, extend the channel-reply mount block (lines 357-362) to:

```go
	// Channel-reply endpoint — bearer token auth via discovery file (no JWT).
	// The channel bridge posts here; auth is validated against the per-PID discovery file.
	if deps.ChannelReply != nil {
		r.Post("/api/channel-reply", deps.ChannelReply.Post)
		r.Get("/api/agents/{pid}/replies", deps.ChannelReply.GetReplies)
	}

	// Channel-stage-output endpoint — bearer token auth via discovery file (no JWT,
	// no Origin/loopback middleware): server-to-server call from the bridge.
	if deps.ChannelStageOutput != nil {
		r.Post("/api/channel-stage-output", deps.ChannelStageOutput.Post)
	}
```

- [ ] **Step 3: Construct + inject in di.go**

In `server/cmd/serve/di.go`, near where `replyStore` and `ChannelReply` are set (lines 259 + 287). After `replyStore := agents.NewReplyStore()`:

```go
	replyStore := agents.NewReplyStore()
	channelStageOutputHandler := agents.NewChannelStageOutputHandler(repo.NewStageRunRepo(entClient))
```

Then in the `api.RouterDeps{...}` literal, next to the `ChannelReply:` line (287):

```go
		ChannelReply:          agents.NewChannelReplyHandler(replyStore),
		ChannelStageOutput:    channelStageOutputHandler,
```

> `entClient` and the `repo` import are already in scope in `di.go` (see `repo.NewStageRunRepo(entClient)` at di.go:200). If `repo` is not yet imported in this file, add `"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"`.

- [ ] **Step 4: Build to verify wiring compiles**

Run: `cd server && go build ./... && task build`
Expected: builds clean, `bin/agent-dashboard` produced.

- [ ] **Step 5: Smoke-test the route with a real server (manual)**

Run the binary, then from another shell:
```bash
curl -s -o /dev/null -w "%{http_code}\n" -X POST http://127.0.0.1:13120/api/channel-stage-output \
  -H 'Content-Type: application/json' -d '{"stageRunId":"nope","output":{"summary":"x"}}'
```
Expected: `404` (unknown stage_run) — proves the route is mounted and **does not** 403 on missing Origin. (A 403 here means it was mounted inside the protected group — move it out.)

- [ ] **Step 6: Commit**

```bash
git add server/internal/api/router.go server/cmd/serve/di.go
git commit -m "feat(pipeline): mount /api/channel-stage-output outside the JWT group"
```

---

## Task 3: completion_detector uses tool-written output

**Files:**
- Modify: `server/internal/pipeline/completion_detector.go` (after the PID-dead check, line ~100)
- Test: `server/internal/pipeline/completion_detector_test.go`

When the agent called `set_stage_output`, `sr.Output` is already populated and already validated. `DetectCompletion` must use it directly — skipping the JSONL scrape. The existing synthetic-adapter branch also uses `sr.Output` but under the reserved key `synthetic_session_file`; exclude that so adapters keep their path.

- [ ] **Step 1: Write the failing test**

Add to `server/internal/pipeline/completion_detector_test.go` (create the file if absent, with `package pipeline` and the imports shown):

```go
package pipeline

import (
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/stretchr/testify/require"
)

func deadPid(int) bool { return false }

func TestDetectCompletion_ToolOutput_UsedDirectly(t *testing.T) {
	sr := &ent.StageRun{
		ID:     "sr-1",
		Stage:  "implementation",
		Pid:    intPtr(1),
		Output: map[string]any{"summary": "from tool", "commits": []any{}, "openItems": []any{}},
	}
	res, err := DetectCompletion(sr, "/tmp", CompletionDeps{
		IsPidAlive: deadPid,
		ReadOutput: func(string, string) (StageOutputRead, error) {
			t.Fatal("scrape path must not run when tool output is present")
			return StageOutputRead{}, nil
		},
	})
	require.NoError(t, err)
	require.Equal(t, "completed", res.Kind)
	require.Equal(t, "from tool", res.Output["summary"])
}

func TestDetectCompletion_SyntheticMarker_NotTreatedAsToolOutput(t *testing.T) {
	sr := &ent.StageRun{
		ID:     "sr-2",
		Stage:  "implementation",
		Pid:    intPtr(1),
		Output: map[string]any{"synthetic_session_file": "/nonexistent/x.jsonl"},
	}
	// synthetic file does not exist → falls through to normal session scan,
	// which finds nothing → failed. Proves the marker is NOT used as tool output.
	res, err := DetectCompletion(sr, "/tmp", CompletionDeps{
		IsPidAlive:  deadPid,
		ReadOutput:  func(string, string) (StageOutputRead, error) { return StageOutputRead{}, nil },
		FindSession: func(string, string) (string, error) { return "", nil },
	})
	require.NoError(t, err)
	require.Equal(t, "failed", res.Kind)
}

func intPtr(n int) *int { return &n }
```

> If `intPtr` already exists in the package's test files, drop the local definition to avoid a redeclaration.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/pipeline/ -run TestDetectCompletion_ToolOutput -v`
Expected: FAIL — the scrape `ReadOutput` is reached (the `t.Fatal` fires), because the early-return does not exist yet.

- [ ] **Step 3: Implement the early-return**

In `server/internal/pipeline/completion_detector.go`, immediately after the PID-alive check (the `if isPidAliveFn(pid) { return ... still_running }` block ends at line ~100) and **before** the synthetic-session block (line ~102), insert:

```go
	// Tool-written stage output: the agent submitted its result via the
	// set_stage_output MCP tool and the endpoint already validated it against
	// the per-stage schema. Use it directly — no JSONL scrape, no retry loop.
	// The synthetic-adapter marker (synthetic_session_file) is handled by the
	// block below, so exclude it here.
	if len(sr.Output) > 0 {
		if _, isSynthetic := sr.Output["synthetic_session_file"]; !isSynthetic {
			return CompletionResult{Kind: "completed", Output: sr.Output}, nil
		}
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd server && go test ./internal/pipeline/ -run TestDetectCompletion -v`
Expected: PASS (both new tests; existing DetectCompletion tests stay green).

- [ ] **Step 5: Full pipeline package test + lint**

Run: `cd server && go test ./internal/pipeline/ && task lint`
Expected: PASS, no lint errors.

- [ ] **Step 6: Commit**

```bash
git add server/internal/pipeline/completion_detector.go server/internal/pipeline/completion_detector_test.go
git commit -m "feat(pipeline): prefer tool-written stage_run.output over JSONL scrape"
```

---

## Task 4: Switch stage prompt contracts to set_stage_output

**Files:**
- Modify: `server/internal/pipeline/stage_prompts.go` (ImplementationPrompt ~line 49, SelfReviewPrompt ~line 81, FinalizationPrompt ~line 108)

Change each stage's closing instruction from "produce a ```json block" to "call `set_stage_output`", keeping the identical schema and a one-line ` ```json ` fallback for non-channel adapters. No behavioural code change — prompt text only.

- [ ] **Step 1: Implementation prompt**

In `ImplementationPrompt`, replace the closing instruction:

```go
When finished, produce a `+"```json```"+` block as your final output:
{"summary": string, "commits": string[], "openItems": string[]}`,
```

with:

```go
When finished, submit your result as your FINAL action by calling the `+"`set_stage_output`"+` MCP tool with an `+"`output`"+` object of exactly this shape:
{"summary": string, "commits": string[], "openItems": string[]}
If `+"`set_stage_output`"+` is unavailable, instead emit the same object as a `+"```json```"+` block.`,
```

- [ ] **Step 2: Self-review prompt**

In `SelfReviewPrompt`, replace:

```go
Respond with a `+"```json```"+` block: {"passed": bool, "findings": [{"severity": "high"|"medium"|"low", "description": string, "file": string|null}], "summary": string}.`,
```

with:

```go
Submit your result as your FINAL action by calling the `+"`set_stage_output`"+` MCP tool with an `+"`output`"+` object of exactly this shape: {"passed": bool, "findings": [{"severity": "high"|"medium"|"low", "description": string, "file": string|null}], "summary": string}. If `+"`set_stage_output`"+` is unavailable, instead emit the same object as a `+"```json```"+` block.`,
```

- [ ] **Step 3: Finalization prompt**

In `FinalizationPrompt`, replace:

```go
Respond with a `+"```json```"+` block: {"summary": string, "insights": string[], "openTodos": string[], "testPlan": string[]}.`,
```

with:

```go
Submit your result as your FINAL action by calling the `+"`set_stage_output`"+` MCP tool with an `+"`output`"+` object of exactly this shape: {"summary": string, "insights": string[], "openTodos": string[], "testPlan": string[]}. If `+"`set_stage_output`"+` is unavailable, instead emit the same object as a `+"```json```"+` block.`,
```

- [ ] **Step 4: Build + run existing prompt tests**

Run: `cd server && go build ./... && go test ./internal/pipeline/ -run Prompt -v`
Expected: builds; any existing prompt tests pass. If a test asserts the old "produce a ```json block" wording, update that assertion to the new wording in the same commit.

- [ ] **Step 5: Commit**

```bash
git add server/internal/pipeline/stage_prompts.go
git commit -m "feat(pipeline): instruct stage agents to call set_stage_output (json block as fallback)"
```

---

## Task 5: Add the set_stage_output tool to the channel bridge

**Files:**
- Modify: `channel/dashboard-channel.ts` (ListTools array ~line 111-169; request handler ~line 202-294, before the final `Unknown tool` return)

Mirror the `dashboard_reply` fetch shape (loopback POST, `Bearer ${TOKEN}`). `stageRunId` auto-injects from `DASHBOARD_STAGE_RUN_ID`. On non-2xx, return an MCP error (`isError: true`) carrying the dashboard's error body so the live agent reads the validation reason and re-calls.

- [ ] **Step 1: Add the tool to the ListTools array**

In the `tools: [ ... ]` array (after the `request_permission` entry), add:

```typescript
    {
      name: 'set_stage_output',
      description:
        'Submit this stage\'s structured result to the dashboard. Call this as your FINAL action. The dashboard validates the output against the stage schema and returns an error if it is malformed — fix and call again. This is the reliable replacement for emitting a ```json block in your final message.',
      inputSchema: {
        type: 'object' as const,
        properties: {
          output: {
            type: 'object',
            description: 'The stage result object, in the exact shape your prompt specified.',
          },
          stageRunId: {
            type: 'string',
            description: 'The current stage_run id (auto-injected from DASHBOARD_STAGE_RUN_ID env)',
          },
        },
        required: ['output'],
      },
    },
```

- [ ] **Step 2: Add the handler branch**

Before the final `return { content: [{ type: 'text', text: `Unknown tool: ${req.params.name}` }] }` line, add:

```typescript
  if (req.params.name === 'set_stage_output') {
    const args = req.params.arguments as { output?: Record<string, unknown>, stageRunId?: string }
    const stageRunId = args.stageRunId || process.env.DASHBOARD_STAGE_RUN_ID
    if (!stageRunId) {
      return {
        content: [{ type: 'text', text: 'No stageRunId — cannot submit stage output. Task is not orchestrator-managed.' }],
        isError: true,
      }
    }
    if (!args.output || typeof args.output !== 'object') {
      return {
        content: [{ type: 'text', text: 'set_stage_output requires an `output` object.' }],
        isError: true,
      }
    }
    try {
      const res = await fetch(`http://127.0.0.1:${DASHBOARD_PORT}/api/channel-stage-output`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${TOKEN}`,
        },
        body: JSON.stringify({ stageRunId, output: args.output }),
      })
      if (!res.ok) {
        const errBody = await res.text().catch(() => '')
        return {
          content: [{ type: 'text', text: `Stage output rejected (${res.status}): ${errBody.slice(0, 500)}. Fix and call set_stage_output again.` }],
          isError: true,
        }
      }
      return { content: [{ type: 'text', text: 'Stage output accepted.' }] }
    }
    catch (err) {
      return {
        content: [{ type: 'text', text: `Could not reach dashboard: ${(err as Error).message}` }],
        isError: true,
      }
    }
  }
```

- [ ] **Step 3: Typecheck / build the bridge**

Run: `cd channel && pnpm typecheck` (or the repo's bridge build — check `channel/package.json` scripts; if compiled via the Go build embed, run `task build`).
Expected: no type errors.

- [ ] **Step 4: Commit**

```bash
git add channel/dashboard-channel.ts
git commit -m "feat(channel): add set_stage_output MCP tool"
```

---

## Task 6: Documentation + layering whitelist

**Files:**
- Modify: `.agent-context/task-pipeline.md` (api→pipeline runtime import whitelist table)

`ChannelStageOutputHandler` (in `api/agents`) imports `pipeline.ValidateStageOutput` at runtime. Per the layering rules, every runtime `api/*`→`pipeline/*` import must be whitelisted with justification.

- [ ] **Step 1: Add the whitelist row**

In `.agent-context/task-pipeline.md`, in the "Runtime import whitelist for routes (api/*) and mcp/*" table, add:

```
| `ValidateStageOutput` | `completion_detector.go` | `api/agents/channel_stage_output.go` |
```

And append to the paragraph below the table:

> `ValidateStageOutput` is a pure schema validator with no state-machine touch — used by the channel-stage-output ingress handler to validate agent-submitted output synchronously.

- [ ] **Step 2: Commit**

```bash
git add .agent-context/task-pipeline.md
git commit -m "docs(pipeline): whitelist ValidateStageOutput for api/agents runtime import"
```

---

## Final Verification

- [ ] **Full test + lint + build**

Run: `cd server && task test && task lint && task build`
Expected: all green; binary builds.

- [ ] **End-to-end manual check (optional but recommended)**

Create a trivial task, let it reach the implementation stage, confirm the stage agent calls `set_stage_output` (visible in its session) and the task progresses **without** the "agent did not produce a ```json output block" error. Confirm a deliberately malformed `self_review` output yields a `422` the agent recovers from.

---

## Self-Review (completed during authoring)

- **Spec coverage:** §1-5 of the spec map to Tasks 1-6; §6 (Origin fix) explicitly deferred to a separate plan per decision. Storage reuse (`stage_runs.output`) = Task 3 + Task 1. Synchronous validation = Task 1. Tool-primary/scrape-fallback = Task 3. Prompt contract = Task 4. Channel tool = Task 5.
- **Placeholder scan:** No "TBD"/"add error handling" left. The two throwaway test stubs (`itoa`, trailing `var _`) are explicitly flagged with removal instructions in their steps.
- **Type consistency:** `NewChannelStageOutputHandler(repo.StageRunRepo)` used identically in Task 1 (def), Task 2 (di.go). `repo.UpdateStageRunInput{Output:}` matches the repo signature. `pipeline.ValidateStageOutput(stage, map[string]any) ValidationResult` (`.OK`/`.Error`) matches completion_detector.go. `CompletionResult{Kind, Output}` matches.
