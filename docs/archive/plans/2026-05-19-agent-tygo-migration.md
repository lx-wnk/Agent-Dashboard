# Agent Struct Tygo Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `sdk/types.go` the single source of truth for the full `Agent` type. After this migration `src/types.ts` re-exports a fully generated `Agent` interface — no manual definition, no `Omit/extend` wrapper.

**Architecture:** Four layers of changes: (1) new typed enums + `BtwMessage` struct in `sdk/types.go`, (2) nullable pointer fields in `Agent`, (3) `BtwMessage` population in `parser.go` + `strPtr` helper in `merger.go`, (4) `src/types.ts` updated to import the generated `Agent`. Each task is independently committable. The `tygo.yaml` comment explaining why `Agent` is not used gets removed at the end.

**Tech Stack:** Go 1.26 (sdk + server modules), tygo (codegen), TypeScript + Vue 3 (frontend), `task generate` to regenerate `src/sdk.generated.ts`

---

## File Map

- Modify: `sdk/types.go` — add `Entrypoint`, `ErrorState`, `BtwMessage` types; update `Agent` fields
- Modify: `server/internal/parser/parser.go` — `SessionData` typed fields + `LastBtw` population
- Modify: `server/internal/merger/merger.go` — `strPtr` helper + updated Agent construction
- Run: `task generate` — regenerates `src/sdk.generated.ts`
- Modify: `tygo.yaml` — remove stale comment about `Agent` not being used
- Modify: `src/types.ts` — replace manual `Agent` with re-export from generated; update imports

---

### Task 1: Add typed enums and BtwMessage to sdk/types.go

**Files:**
- Modify: `sdk/types.go`

- [ ] **Step 1: Add Entrypoint type with const block**

In `sdk/types.go`, after the `AgentStatus` const block (after line `AgentStatusIdle AgentStatus = "idle"`), add:

```go
// Entrypoint describes how the Claude Code process was launched.
type Entrypoint string

const (
	EntrypointCLI     Entrypoint = "cli"
	EntrypointDesktop Entrypoint = "desktop"
	EntrypointUnknown Entrypoint = "unknown"
)

// ErrorState describes a recognisable error condition seen in the session log.
type ErrorState string

const (
	ErrorStateQuotaExhausted ErrorState = "quota_exhausted"
	ErrorStateRateLimited    ErrorState = "rate_limited"
	ErrorStateAuthFailed     ErrorState = "auth_failed"
)

// BtwMessage is the last assistant text that appeared alongside tool calls.
// Message is the text content; Response is reserved for future use.
type BtwMessage struct {
	Message  string  `json:"message"`
	Response *string `json:"response"`
}
```

- [ ] **Step 2: Update Agent struct fields**

In `sdk/types.go`, replace the `Agent` struct definition with:

```go
// Agent is the unified view of a running Claude Code process.
type Agent struct {
	PID                       int            `json:"pid"`
	SessionID                 string         `json:"sessionId"`
	ProjectPath               string         `json:"projectPath"`
	ProjectName               string         `json:"projectName"`
	CWD                       string         `json:"cwd"`
	Entrypoint                Entrypoint     `json:"entrypoint"`
	Status                    AgentStatus    `json:"status"`
	Uptime                    int64          `json:"uptime"`
	LastActivity              string         `json:"lastActivity"`
	CurrentAction             *string        `json:"currentAction"`
	LastTools                 []string       `json:"lastTools"`
	Tasks                     []TaskInfo     `json:"tasks"`
	Subagents                 []SubAgent     `json:"subagents"`
	TokenUsage                TokenUsage     `json:"tokenUsage"`
	CostEstimate              float64        `json:"costEstimate"`
	CacheCreationCostEstimate float64        `json:"cacheCreationCostEstimate"`
	CacheReadCostEstimate     float64        `json:"cacheReadCostEstimate"`
	HealthScore               int            `json:"healthScore"`
	Model                     *string        `json:"model"`
	CodeVersion               *string        `json:"codeVersion"`
	ConversationTurns         int            `json:"conversationTurns"`
	ToolCounts                map[string]int `json:"toolCounts"`
	Meta                      *SessionMeta   `json:"meta"`
	ChannelAvailable          bool           `json:"channelAvailable"`
	LastOutput                *string        `json:"lastOutput"`
	ConvergenceAlert          bool           `json:"convergenceAlert"`
	ConvergenceToolName       *string        `json:"convergenceToolName"`
	ErrorState                *ErrorState    `json:"errorState"`
	PipelineTaskID            string         `json:"pipelineTaskId,omitempty"`
	PipelineTaskTitle         string         `json:"pipelineTaskTitle,omitempty"`
	Machine                   string         `json:"machine,omitempty"`
	LastBtw                   *BtwMessage    `json:"lastBtw"`
}
```

- [ ] **Step 3: Build sdk module to verify**

```bash
cd sdk && go build ./...
```

Expected: exits 0 with no output. (Server will fail until merger is updated — build sdk only for now.)

- [ ] **Step 4: Commit**

```bash
git add sdk/types.go
git commit -m "feat(sdk): add Entrypoint/ErrorState enums, BtwMessage struct, nullable Agent pointer fields"
```

---

### Task 2: Update parser — typed fields + BtwMessage population

**Files:**
- Modify: `server/internal/parser/parser.go`

- [ ] **Step 1: Update SessionData struct**

In `server/internal/parser/parser.go`, replace the `SessionData` struct:

```go
// SessionData is the parsed output of a Claude Code JSONL session log.
type SessionData struct {
	SessionID           string
	ProjectPath         string
	Entrypoint          sdk.Entrypoint
	LastActivity        time.Time
	CurrentAction       string
	LastTools           []string
	Tasks               []sdk.TaskInfo
	TokenUsage          sdk.TokenUsage
	Model               string
	ConversationTurns   int
	ToolCounts          map[string]int
	LastOutput          string
	ConvergenceAlert    bool
	ConvergenceToolName string
	ErrorState          sdk.ErrorState
	Meta                *sdk.SessionMeta
	LastBtw             *sdk.BtwMessage
}
```

- [ ] **Step 2: Update ParseSessionFile to initialise Entrypoint**

In `ParseSessionFile`, the existing initialisation sets `Entrypoint: "unknown"`. Change it to use the typed constant:

```go
data := &SessionData{
    ToolCounts:   make(map[string]int),
    Entrypoint:   sdk.EntrypointUnknown,
    LastActivity: time.Now().Add(-24 * time.Hour),
}
```

- [ ] **Step 3: Update ErrorState assignments**

In the `case "text":` block inside the block-parsing loop, replace the three string literals:

```go
case "text":
    if b.Text != "" {
        data.LastOutput = scrubSecrets(b.Text)
        switch {
        case quotaRE.MatchString(b.Text):
            data.ErrorState = sdk.ErrorStateQuotaExhausted
        case rateRE.MatchString(b.Text):
            data.ErrorState = sdk.ErrorStateRateLimited
        case authRE.MatchString(b.Text):
            data.ErrorState = sdk.ErrorStateAuthFailed
        }
    }
```

- [ ] **Step 4: Add BtwMessage population in the block-parsing loop**

The BtwMessage captures the last assistant text that appeared in a turn that also contained tool_use blocks.

Replace the inner block-parsing section (the `var blocks []toolUseBlock` through end of the if block) with:

```go
var blocks []toolUseBlock
if err := json.Unmarshal(msg.Content, &blocks); err == nil {
    var btwText string
    hasToolUse := false
    for _, b := range blocks {
        switch b.Type {
        case "tool_use":
            hasToolUse = true
            data.ToolCounts[b.Name]++
            recentToolNames = append(recentToolNames, b.Name)
            data.CurrentAction = b.Name
            if b.Name == "TodoWrite" {
                var inp todoInput
                if err := json.Unmarshal(b.Input, &inp); err == nil {
                    tasks := make([]sdk.TaskInfo, 0, len(inp.Todos))
                    for _, td := range inp.Todos {
                        tasks = append(tasks, sdk.TaskInfo{
                            ID:      td.ID,
                            Subject: td.Content,
                            Status:  td.Status,
                        })
                    }
                    data.Tasks = tasks
                }
            }
        case "text":
            if b.Text != "" {
                btwText = scrubSecrets(b.Text)
                data.LastOutput = btwText
                switch {
                case quotaRE.MatchString(b.Text):
                    data.ErrorState = sdk.ErrorStateQuotaExhausted
                case rateRE.MatchString(b.Text):
                    data.ErrorState = sdk.ErrorStateRateLimited
                case authRE.MatchString(b.Text):
                    data.ErrorState = sdk.ErrorStateAuthFailed
                }
            }
        }
    }
    if hasToolUse && btwText != "" {
        data.LastBtw = &sdk.BtwMessage{Message: btwText}
    }
}
```

- [ ] **Step 5: Build server to check for compile errors**

```bash
cd server && go build ./...
```

Expected: exits 0. (Merger uses old string types on Agent — will fail. Fix in Task 3.)

If you see errors in `merger.go` about type mismatches, that's expected — proceed to Task 3 immediately.

- [ ] **Step 6: Commit parser changes**

```bash
git add server/internal/parser/parser.go
git commit -m "feat(parser): typed Entrypoint/ErrorState fields, BtwMessage population for inter-tool text"
```

---

### Task 3: Update merger — strPtr helper + typed Agent construction

**Files:**
- Modify: `server/internal/merger/merger.go`

- [ ] **Step 1: Add strPtr helper**

At the bottom of `merger.go`, after the closing brace of `GetAgents`, add:

```go
// strPtr returns nil if s is empty, otherwise a pointer to a copy of s.
// Used to convert empty-means-absent string fields to nullable pointer fields.
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
```

- [ ] **Step 2: Update Agent construction in GetAgents**

Replace the `agents[i] = sdk.Agent{...}` block:

```go
agents[i] = sdk.Agent{
    PID:                       proc.PID,
    SessionID:                 session.SessionID,
    ProjectPath:               proc.CWD,
    ProjectName:               filepath.Base(proc.CWD),
    CWD:                       proc.CWD,
    Entrypoint:                session.Entrypoint,
    Status:                    CalculateStatus(session.LastActivity),
    Uptime:                    proc.Uptime,
    LastActivity:              session.LastActivity.Format(time.RFC3339),
    CurrentAction:             strPtr(session.CurrentAction),
    LastTools:                 append(make([]string, 0), session.LastTools...),
    Tasks:                     append(make([]sdk.TaskInfo, 0), session.Tasks...),
    Subagents:                 []sdk.SubAgent{},
    TokenUsage:                session.TokenUsage,
    CostEstimate:              cost,
    CacheCreationCostEstimate: cacheCreate,
    CacheReadCostEstimate:     cacheRead,
    Model:                     strPtr(session.Model),
    ConversationTurns:         session.ConversationTurns,
    ToolCounts:                session.ToolCounts,
    Meta:                      session.Meta,
    ConvergenceAlert:          session.ConvergenceAlert,
    ConvergenceToolName:       strPtr(session.ConvergenceToolName),
    LastOutput:                strPtr(session.LastOutput),
    LastBtw:                   session.LastBtw,
    ErrorState: func() *sdk.ErrorState {
        if session.ErrorState == "" {
            return nil
        }
        es := session.ErrorState
        return &es
    }(),
}
```

- [ ] **Step 3: Build all modules**

```bash
task build
```

Expected: exits 0. Binary at `bin/agent-dashboard`.

- [ ] **Step 4: Run all Go tests**

```bash
task test
```

Expected: all tests pass. If merger tests fail due to field type changes, update the test fixtures to use pointer values (e.g., `Model: strPtr("claude-opus-4")` → use the helper or inline: `Model: func() *string { s := "x"; return &s }()`).

- [ ] **Step 5: Commit**

```bash
git add server/internal/merger/merger.go
git commit -m "feat(merger): strPtr helper + typed Entrypoint/ErrorState/nullable pointer fields in Agent construction"
```

---

### Task 4: Regenerate sdk.generated.ts

**Files:**
- Run: `task generate`
- Verify: `src/sdk.generated.ts`

- [ ] **Step 1: Run codegen**

```bash
task generate
```

Expected: no errors. `src/sdk.generated.ts` is updated.

- [ ] **Step 2: Verify generated output**

```bash
grep -A5 "export type Entrypoint\|export type ErrorState\|export interface BtwMessage\|export interface Agent" src/sdk.generated.ts | head -60
```

Expected output to include:

```
export const EntrypointCLI = "cli";
export const EntrypointDesktop = "desktop";
export const EntrypointUnknown = "unknown";
export type Entrypoint = typeof EntrypointCLI | typeof EntrypointDesktop | typeof EntrypointUnknown;

export const ErrorStateQuotaExhausted = "quota_exhausted";
export const ErrorStateRateLimited = "rate_limited";
export const ErrorStateAuthFailed = "auth_failed";
export type ErrorState = typeof ErrorStateQuotaExhausted | typeof ErrorStateRateLimited | typeof ErrorStateAuthFailed;

export interface BtwMessage {
  message: string;
  response: string | null;
}

export interface Agent {
  ...
  entrypoint: Entrypoint;
  currentAction: string | null;
  model: string | null;
  errorState: ErrorState | null;
  lastBtw: BtwMessage | null;
  ...
}
```

If `response` in `BtwMessage` is generated as `string | undefined` rather than `string | null`, that is acceptable — the TS frontend handles both. No action needed.

- [ ] **Step 3: Commit regenerated file**

```bash
git add src/sdk.generated.ts
git commit -m "chore(codegen): regenerate sdk.generated.ts with Entrypoint/ErrorState enums, BtwMessage, nullable Agent fields"
```

---

### Task 5: Migrate src/types.ts — replace manual Agent with generated

**Files:**
- Modify: `src/types.ts`
- Modify: `tygo.yaml`

- [ ] **Step 1: Update tygo.yaml to remove stale comment**

In `tygo.yaml`, replace the `NOTE` comment about `Agent` not being used:

```yaml
packages:
  - path: github.com/lx-wnk/agent-dashboard/sdk
    output_path: src/sdk.generated.ts
    enum_style: union
    type_mappings:
      # AgentStatus is handled by enum_style: union (Go const block → TS union).
      # Add overrides here if tygo output diverges from the canonical TS definition.
```

- [ ] **Step 2: Update imports in src/types.ts**

Replace the entire import block at the top of `src/types.ts` (lines 1–16 — everything up to and including `export type { TokenUsage, AgentStatus }`) with:

```typescript
// Types generated from sdk/types.go via tygo — do not edit directly.
// Run `task generate` to regenerate after changing sdk/types.go.
import {
  AgentStatusActive,
  AgentStatusWaiting,
  AgentStatusIdle,
} from './sdk.generated'
import type {
  TokenUsage,
  AgentStatus,
  Entrypoint,
  ErrorState,
  BtwMessage,
  SessionMeta as _SessionMetaBase,
  SubAgent as _SubAgentBase,
  TaskInfo as _TaskInfoBase,
} from './sdk.generated'

export type { TokenUsage, AgentStatus, Entrypoint, ErrorState, BtwMessage }
```

- [ ] **Step 3: Remove the manual Agent interface from src/types.ts**

Delete the entire `export interface Agent { ... }` block. It spans from `// NOTE: sdk.generated.ts also exports...` through the closing `}` of the Agent interface (approximately lines 31–90 in the current file).

- [ ] **Step 4: Add Agent re-export after the AGENT_STATUSES const**

After the `AGENT_STATUSES` and before the `ChannelReply` interface, add:

```typescript
// Agent is the unified view of a running Claude Code process — fully generated from sdk/types.go.
export type { Agent } from './sdk.generated'
```

- [ ] **Step 5: Run typecheck**

```bash
pnpm typecheck
```

Expected: zero errors.

If you see errors on files that use `agent.errorState === 'quota_exhausted'` — these comparisons are still valid because `ErrorState` is a string union. No changes needed.

If you see errors on `agent.currentAction.length` (treating nullable as non-null) — add `?.` or a null guard at the callsite.

If you see errors on `agent.model` used as `string` where `string | null` is expected — add `?? ''` or a null guard.

- [ ] **Step 6: Run frontend unit tests**

```bash
pnpm test
```

Expected: all tests pass.

- [ ] **Step 7: Commit**

```bash
git add src/types.ts tygo.yaml
git commit -m "feat(types): replace manual Agent interface with tygo-generated type — full SSOT migration (ARCH-05 pt.2)"
```

---

### Task 6: Final build and PR

- [ ] **Step 1: Full clean build + tests**

```bash
task build && task test && pnpm test && pnpm typecheck
```

Expected: all pass with zero errors.

- [ ] **Step 2: Verify dashboard works**

```bash
task dev
```

Open `http://localhost:5173`. Verify agents appear with correct model names, currentAction, errorState values. Check that `AgentCard.vue` renders without console errors.

- [ ] **Step 3: Create PR**

```bash
git push -u origin feat/agent-tygo-migration
gh pr create \
  --title "feat(arch): full Agent struct tygo migration — SSOT for Go→TS type sync (ARCH-05 pt.2)" \
  --base upcoming \
  --body "$(cat <<'EOF'
## Summary
- `sdk/types.go` is now the single source of truth for the complete `Agent` type
- Added typed string enums: `Entrypoint` (`cli`/`desktop`/`unknown`), `ErrorState` (`quota_exhausted`/`rate_limited`/`auth_failed`)
- Added `BtwMessage` struct — captures assistant text that appears alongside tool calls; populated in parser
- Nullable fields (`CurrentAction`, `Model`, `CodeVersion`, `LastOutput`, `ConvergenceToolName`, `ErrorState`) changed to pointer types in Go; `strPtr` helper in merger converts empty→nil
- `src/types.ts` manual `Agent` interface removed — replaced with `export type { Agent } from './sdk.generated'`
- `src/sdk.generated.ts` regenerated via `task generate`

## Test plan
- [ ] `task build` passes
- [ ] `task test` passes (Go)
- [ ] `pnpm test` passes (Vitest)
- [ ] `pnpm typecheck` zero errors
- [ ] Agents appear correctly in the dashboard with model/currentAction/errorState values

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```
