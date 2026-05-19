# Design: Full Agent Struct tygo Migration

**Date:** 2026-05-19  
**Status:** Approved

---

## Goal

Make `sdk/types.go` the single source of truth for the full `Agent` type. After this migration `src/types.ts` exports a generated `Agent` — no manual interface definition, no `Omit/extend` wrapper.

The partial tygo migration (TokenUsage, SessionMeta, SubAgent, TaskInfo, AgentStatus) is already done. This spec covers the remaining work: typed enums for `Entrypoint`/`ErrorState`, nullable pointer fields, and `BtwMessage`.

---

## Changes to `sdk/types.go`

### 1. New typed string enums

```go
type Entrypoint string

const (
    EntrypointCLI     Entrypoint = "cli"
    EntrypointDesktop Entrypoint = "desktop"
    EntrypointUnknown Entrypoint = "unknown"
)

type ErrorState string

const (
    ErrorStateQuotaExhausted ErrorState = "quota_exhausted"
    ErrorStateRateLimited    ErrorState = "rate_limited"
    ErrorStateAuthFailed     ErrorState = "auth_failed"
)
```

No `ErrorStateNone = ""` — absence of error is expressed as `nil` (`*ErrorState`).

### 2. `BtwMessage` struct

Represents the last assistant text that appeared alongside tool calls ("between tool calls" commentary).

```go
type BtwMessage struct {
    Message  string  `json:"message"`
    Response *string `json:"response"` // reserved for future use; nil = absent
}
```

### 3. `Agent` field changes

| Field | Before | After | Reason |
|-------|--------|-------|--------|
| `Entrypoint` | `string` | `Entrypoint` | Typed enum |
| `ErrorState` | `string` | `*ErrorState` | Nullable; nil = no error |
| `CurrentAction` | `string` | `*string` | nil when no active tool |
| `Model` | `string` | `*string` | nil when model unknown |
| `CodeVersion` | `string` | `*string` | nil when unknown |
| `LastOutput` | `string` | `*string` | nil when no output yet |
| `ConvergenceToolName` | `string` | `*string` | nil when no convergence |
| `LastBtw` | *(absent)* | `*BtwMessage` | New; nil = no inter-tool text |

`ChannelAvailable`, `HealthScore`, `ConvergenceAlert`, `PipelineTaskID`, `PipelineTaskTitle`, `Machine` are unchanged.

---

## Changes to `server/internal/parser/parser.go`

### `SessionData` struct

Update field types to match new sdk types:

```go
Entrypoint          sdk.Entrypoint
ErrorState          sdk.ErrorState  // empty string = no error; merger converts to *sdk.ErrorState
CurrentAction       string          // keep as plain string; merger does nil conversion
Model               string          // same
CodeVersion         string          // same
LastOutput          string          // same
ConvergenceToolName string          // same
LastBtw             *sdk.BtwMessage // new
```

`SessionData` keeps plain strings for most fields; the merger handles `nil` promotion. Only `Entrypoint` and `ErrorState` adopt the typed aliases here (they're already string-typed, so this is backward-compatible).

### BtwMessage population

In the content block loop, when an assistant turn contains **both** `text` and `tool_use` blocks, the text block is an inter-tool commentary:

```go
// Inside the blocks loop (after type switch):
var btwText string
hasToolUse := false
for _, b := range blocks {
    switch b.Type {
    case "tool_use":
        hasToolUse = true
        // ... existing tool_use handling ...
    case "text":
        btwText = scrubSecrets(b.Text)
        // ... existing text handling ...
    }
}
if hasToolUse && btwText != "" {
    data.LastBtw = &sdk.BtwMessage{Message: btwText}
}
```

`BtwMessage.Response` is always `nil` for now (reserved for a future phase that tracks tool result summaries).

---

## Changes to `server/internal/merger/merger.go`

Add a private helper:

```go
func strPtr(s string) *string {
    if s == "" {
        return nil
    }
    return &s
}
```

Update `Agent` construction:

```go
sdk.Agent{
    // ...
    Entrypoint:          sdk.Entrypoint(session.Entrypoint),
    CurrentAction:       strPtr(session.CurrentAction),
    Model:               strPtr(session.Model),
    CodeVersion:         strPtr(session.CodeVersion),
    LastOutput:          strPtr(session.LastOutput),
    ConvergenceToolName: strPtr(session.ConvergenceToolName),
    LastBtw:             session.LastBtw,
    ErrorState: func() *sdk.ErrorState {
        if session.ErrorState == "" {
            return nil
        }
        es := session.ErrorState
        return &es
    }(),
}
```

---

## Codegen (`task generate`)

After sdk/types.go changes: run `task generate`. tygo output (`src/sdk.generated.ts`) will include:

- `Entrypoint` type as `"cli" | "desktop" | "unknown"`
- `ErrorState` type as `"quota_exhausted" | "rate_limited" | "auth_failed"`
- `BtwMessage` interface
- `Agent` interface with all fields using generated types, pointer fields as `type | null`

Verify the generated output before updating `src/types.ts`.

---

## Changes to `src/types.ts`

Remove the manual `Agent` interface entirely. Replace with:

```typescript
export type { Agent } from './sdk.generated'
```

Keep:
- `AGENT_STATUSES` const (runtime array — tygo cannot generate runtime values)
- All other interfaces (`ChannelReply`, `GitStatus`, `OutputMessage`, `PipelineStage`, etc.)
- The existing re-exports of `TokenUsage`, `AgentStatus`, `SessionMeta`, `SubAgent`, `TaskInfo` from `sdk.generated`

The `AgentStatus` import block at the top (`AgentStatusActive`, `AgentStatusWaiting`, `AgentStatusIdle`) stays — it feeds `AGENT_STATUSES`.

### Callsite updates

After removing the manual `Agent` interface, field types change as follows — update all affected callers:

| Field | Old TS type | New TS type |
|-------|------------|------------|
| `currentAction` | `string \| null` | `string \| null` *(unchanged)* |
| `model` | `string \| null` | `string \| null` *(unchanged)* |
| `errorState` | `'quota_exhausted' \| 'rate_limited' \| 'auth_failed' \| null` | `ErrorState \| null` *(compatible union)* |
| `entrypoint` | `'cli' \| 'desktop' \| 'unknown'` | `Entrypoint` *(same values)* |
| `lastBtw` | `{ message: string, response: string \| null } \| null` | `BtwMessage \| null` |

Run `pnpm typecheck` to surface any incompatible usages. Expect minor fixes in `AgentCard.vue` and `AgentRow.vue` where `errorState` is compared to string literals — those comparisons remain valid since `ErrorState` is a string enum.

---

## Testing

- `pnpm typecheck` — zero errors after `src/types.ts` migration
- `pnpm test` — all Vitest tests pass
- `task test` — all Go tests pass (merger/parser field changes covered by existing tests)
- Manual: run `task dev`, verify agents appear in the dashboard with correct `model`, `currentAction`, `errorState` values

---

## Out of Scope

- `ChannelAvailable` population (separate feature — F-16 arch finding)
- `HealthScore` / `CodeVersion` population (separate feature)
- `BtwMessage.Response` population (reserved field, always nil for now)
- CI drift-check for `sdk.generated.ts` (follow-up)
- Full `Agent` OIDC-style enum migration for `entrypoint` variants beyond the three current values
