# Go Rebuild — Phase 4: MCP Endpoint

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the stateless StreamableHTTP MCP server at `POST /api/mcp` with all 19 tools and 4 scope tiers — a direct Go port of the TypeScript `server/mcp/` package.

**Architecture:** Custom JSON-RPC 2.0 over HTTP handler (no external MCP library). Each POST is self-contained — no server-side session state. A `ToolRegistry` maps tool name → handler; a chi middleware extracts the Bearer token, hashes it (SHA-256), looks it up in `api_keys`, resolves implied scopes, and attaches them to the request context. Tools call existing ent repos directly.

**Tech Stack:** Go 1.26, go-chi/chi v5, entgo/ent, google/uuid, crypto/sha256, existing internal repos

---

## File Map

```
server/internal/
  db/repo/
    task_dependency_repo.go          ← new: DependencyRepo interface + AddDependency, RemoveDependency
    api_key_repo.go                  ← modify: add GetByID to ApiKeyRepo interface + impl
  mcp/
    auth.go                          ← TOOL_SCOPE_MAP, resolveScopes, McpAuthMiddleware, context helpers
    jsonrpc.go                       ← JSON-RPC 2.0 types, MCPHandler (chi http.Handler)
    registry.go                      ← ToolRegistry, ToolDef, ToolResult, ToolInput, mcpError, ok helpers
    server.go                        ← BuildMCPServer — wires all tools into registry + returns MCPHandler
    tools/
      read.go                        ← list_tasks, get_task, list_stage_runs, list_audit, list_permission_requests
      write.go                       ← create_task, update_task, delete_task, manage_task, add_dependency, remove_dependency
      control.go                     ← progress_task, cancel_task, retry_task, grant_permission, resolve_permission_request
      keys.go                        ← list_api_keys, create_api_key, revoke_api_key
server/internal/api/
  router.go                          ← add POST /api/mcp using MCPHandler (already imports mcp if wired via RouterDeps)
server/cmd/serve/
  wire_gen.go                        ← add MCPHandler to RouterDeps
```

---

## Scope + Tool Map (from TypeScript TOOL_SCOPE_MAP)

```
tasks:read     → list_tasks, get_task, list_stage_runs, list_audit, list_permission_requests
tasks:write    → create_task, update_task, delete_task, manage_task, add_dependency, remove_dependency
pipeline:control → progress_task, cancel_task, retry_task, grant_permission, resolve_permission_request
keys:manage    → list_api_keys, create_api_key, revoke_api_key
```

Scope implication: `keys:manage` → all; `tasks:write` → `tasks:read`; `pipeline:control` → `tasks:read`.

---

## Task 1: TaskDependency repo + ApiKeyRepo.GetByID

**Files:**
- Create: `server/internal/db/repo/task_dependency_repo.go`
- Modify: `server/internal/db/repo/api_key_repo.go`

- [ ] **Step 1: Write failing tests for DependencyRepo**

```go
// server/internal/db/repo/task_dependency_repo_test.go
package repo_test

import (
    "context"
    "testing"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
    "github.com/stretchr/testify/require"
)

func TestDependencyRepo(t *testing.T) {
    ctx := context.Background()
    client := newTestClient(t)
    taskRepo := repo.NewTaskRepo(client)
    depRepo := repo.NewDependencyRepo(client)

    parent := mustCreateTask(t, ctx, taskRepo, "parent", "/tmp")
    child  := mustCreateTask(t, ctx, taskRepo, "child",  "/tmp")

    dep, err := depRepo.Add(ctx, child.ID, parent.ID, "done", "on_hold")
    require.NoError(t, err)
    require.Equal(t, child.ID, dep.TaskID)
    require.Equal(t, parent.ID, dep.DependsOnID)

    removed, err := depRepo.Remove(ctx, child.ID, parent.ID)
    require.NoError(t, err)
    require.True(t, removed)

    // duplicate dependency returns error
    _, err = depRepo.Add(ctx, child.ID, parent.ID, "done", "on_hold")
    require.NoError(t, err)
    _, err = depRepo.Add(ctx, child.ID, parent.ID, "done", "on_hold")
    require.Error(t, err)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd server && go test ./internal/db/repo/... -run TestDependencyRepo -v
```
Expected: FAIL (DependencyRepo not found)

- [ ] **Step 3: Implement DependencyRepo**

```go
// server/internal/db/repo/task_dependency_repo.go
package repo

import (
    "context"
    "fmt"

    "github.com/google/uuid"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/ent/taskdependency"
)

type DependencyRepo interface {
    Add(ctx context.Context, taskID, dependsOnID, requiredStage, onCancelAction string) (*ent.TaskDependency, error)
    Remove(ctx context.Context, taskID, dependsOnID string) (bool, error)
}

type entDependencyRepo struct{ client *ent.Client }

func NewDependencyRepo(client *ent.Client) DependencyRepo {
    return &entDependencyRepo{client: client}
}

func (r *entDependencyRepo) Add(ctx context.Context, taskID, dependsOnID, requiredStage, onCancelAction string) (*ent.TaskDependency, error) {
    dep, err := r.client.TaskDependency.Create().
        SetID(uuid.New().String()).
        SetTaskID(taskID).
        SetDependsOnID(dependsOnID).
        SetRequiredStage(requiredStage).
        SetOnCancelAction(onCancelAction).
        Save(ctx)
    if err != nil {
        return nil, fmt.Errorf("dependency.Add: %w", err)
    }
    return dep, nil
}

func (r *entDependencyRepo) Remove(ctx context.Context, taskID, dependsOnID string) (bool, error) {
    n, err := r.client.TaskDependency.Delete().
        Where(
            taskdependency.TaskID(taskID),
            taskdependency.DependsOnID(dependsOnID),
        ).Exec(ctx)
    if err != nil {
        return false, fmt.Errorf("dependency.Remove: %w", err)
    }
    return n > 0, nil
}
```

- [ ] **Step 4: Add GetByID to ApiKeyRepo**

In `api_key_repo.go`, add to the interface:
```go
GetByID(ctx context.Context, id string) (*ent.ApiKey, error)
```

And the implementation:
```go
func (r *entApiKeyRepo) GetByID(ctx context.Context, id string) (*ent.ApiKey, error) {
    k, err := r.client.ApiKey.Get(ctx, id)
    if err != nil {
        return nil, fmt.Errorf("apikey.GetByID: %w", err)
    }
    return k, nil
}
```

- [ ] **Step 5: Run tests to verify pass**

```bash
cd server && go test ./internal/db/repo/... -v
```
Expected: PASS all

- [ ] **Step 6: Commit**

```bash
git add server/internal/db/repo/task_dependency_repo.go server/internal/db/repo/task_dependency_repo_test.go server/internal/db/repo/api_key_repo.go
git commit -m "feat(mcp): add DependencyRepo + ApiKeyRepo.GetByID for MCP tools"
```

---

## Task 2: MCP auth middleware + JSON-RPC core + ToolRegistry

**Files:**
- Create: `server/internal/mcp/auth.go`
- Create: `server/internal/mcp/jsonrpc.go`
- Create: `server/internal/mcp/registry.go`

- [ ] **Step 1: Write failing test for scope resolution**

```go
// server/internal/mcp/auth_test.go
package mcp_test

import (
    "testing"
    "github.com/lx-wnk/agent-dashboard/server/internal/mcp"
    "github.com/stretchr/testify/require"
)

func TestResolveScopes(t *testing.T) {
    scopes := mcp.ResolveScopes([]string{"tasks:write"})
    require.True(t, scopes["tasks:write"])
    require.True(t, scopes["tasks:read"])   // implied
    require.False(t, scopes["keys:manage"]) // not implied

    scopes2 := mcp.ResolveScopes([]string{"keys:manage"})
    require.True(t, scopes2["tasks:read"])
    require.True(t, scopes2["tasks:write"])
    require.True(t, scopes2["pipeline:control"])
    require.True(t, scopes2["keys:manage"])
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd server && go test ./internal/mcp/... -run TestResolveScopes -v
```
Expected: compile error (package not found)

- [ ] **Step 3: Implement auth.go**

```go
// server/internal/mcp/auth.go
package mcp

import (
    "context"
    "crypto/sha256"
    "encoding/hex"
    "net/http"

    "github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// Scope map: tool name → required scope.
var ToolScopeMap = map[string]string{
    // tasks:read
    "list_tasks": "tasks:read", "get_task": "tasks:read",
    "list_stage_runs": "tasks:read", "list_audit": "tasks:read",
    "list_permission_requests": "tasks:read",
    // tasks:write
    "create_task": "tasks:write", "update_task": "tasks:write",
    "delete_task": "tasks:write", "manage_task": "tasks:write",
    "add_dependency": "tasks:write", "remove_dependency": "tasks:write",
    // pipeline:control
    "progress_task": "pipeline:control", "cancel_task": "pipeline:control",
    "retry_task": "pipeline:control", "grant_permission": "pipeline:control",
    "resolve_permission_request": "pipeline:control",
    // keys:manage
    "list_api_keys": "keys:manage", "create_api_key": "keys:manage",
    "revoke_api_key": "keys:manage",
}

var scopeImplies = map[string][]string{
    "tasks:read":      {},
    "tasks:write":     {"tasks:read"},
    "pipeline:control":{"tasks:read"},
    "keys:manage":     {"tasks:read", "tasks:write", "pipeline:control"},
}

// ResolveScopes expands a set of scopes with their implied scopes.
func ResolveScopes(scopes []string) map[string]bool {
    result := make(map[string]bool)
    for _, s := range scopes {
        result[s] = true
        for _, implied := range scopeImplies[s] {
            result[implied] = true
        }
    }
    return result
}

func HashToken(token string) string {
    h := sha256.Sum256([]byte(token))
    return hex.EncodeToString(h[:])
}

// GenerateAPIToken returns a new random mcp_<hex> token.
func GenerateAPIToken() string {
    b := make([]byte, 32)
    // crypto/rand is imported below if needed
    if _, err := cryptoRandRead(b); err != nil {
        panic("mcp: cannot generate token: " + err.Error())
    }
    return "mcp_" + hex.EncodeToString(b)
}

type mcpAuthKey struct{}

type MCPAuthInfo struct {
    KeyID  string
    Scopes map[string]bool
    UserID string // empty string = no user
}

func AuthFromContext(ctx context.Context) *MCPAuthInfo {
    v, _ := ctx.Value(mcpAuthKey{}).(*MCPAuthInfo)
    return v
}

// McpAuthMiddleware reads Bearer token → hash lookup → scope resolution.
func McpAuthMiddleware(keyRepo repo.ApiKeyRepo) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            header := r.Header.Get("Authorization")
            if len(header) < 8 || header[:7] != "Bearer " {
                http.Error(w, `{"error":"Missing or invalid Authorization header"}`, http.StatusUnauthorized)
                return
            }
            token := header[7:]
            hash := HashToken(token)
            key, err := keyRepo.GetByHash(r.Context(), hash)
            if err != nil {
                http.Error(w, `{"error":"Invalid or revoked API key"}`, http.StatusUnauthorized)
                return
            }
            go func() { _ = keyRepo.TouchLastUsed(r.Context(), key.ID) }() //nolint:errcheck
            info := &MCPAuthInfo{
                KeyID:  key.ID,
                Scopes: ResolveScopes(key.Scopes),
            }
            if key.UserID != nil {
                info.UserID = *key.UserID
            }
            next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), mcpAuthKey{}, info)))
        })
    }
}
```

Note: replace `cryptoRandRead` with `crypto/rand.Read` in the actual import section.

- [ ] **Step 4: Implement registry.go**

```go
// server/internal/mcp/registry.go
package mcp

import (
    "context"
    "encoding/json"
    "fmt"
)

// ToolResult is the MCP content block returned by every tool.
type ToolResult struct {
    Content []ContentBlock `json:"content"`
}

type ContentBlock struct {
    Type string `json:"type"`
    Text string `json:"text"`
}

// OK wraps data as a JSON text content block.
func OK(data any) (*ToolResult, error) {
    b, err := json.Marshal(data)
    if err != nil {
        return nil, fmt.Errorf("mcp: marshal result: %w", err)
    }
    return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

// MCPError returns an error that the JSON-RPC handler surfaces as a tool error.
type MCPError struct{ Message string }
func (e *MCPError) Error() string { return e.Message }
func Fail(msg string) error       { return &MCPError{Message: msg} }

// ToolHandler is the function signature every tool implements.
type ToolHandler func(ctx context.Context, args map[string]any) (*ToolResult, error)

// ToolDef holds a tool's metadata and its JSON Schema for input validation.
type ToolDef struct {
    Name        string
    Description string
    InputSchema map[string]any // JSON Schema object
    Handler     ToolHandler
}

// ToolRegistry maps tool names to definitions.
type ToolRegistry map[string]*ToolDef

// Register adds a tool to the registry.
func (r ToolRegistry) Register(def *ToolDef) {
    r[def.Name] = def
}

// StringArg extracts a required string argument.
func StringArg(args map[string]any, key string) (string, error) {
    v, ok := args[key]
    if !ok {
        return "", Fail(key + " is required")
    }
    s, ok := v.(string)
    if !ok {
        return "", Fail(key + " must be a string")
    }
    return s, nil
}

// OptionalString extracts an optional string argument (returns empty string if absent).
func OptionalString(args map[string]any, key string) string {
    v, _ := args[key].(string)
    return v
}

// OptionalBool extracts an optional bool argument.
func OptionalBool(args map[string]any, key string) bool {
    v, _ := args[key].(bool)
    return v
}

// OptionalFloat64 extracts an optional number argument.
func OptionalFloat64(args map[string]any, key string) (float64, bool) {
    v, ok := args[key]
    if !ok {
        return 0, false
    }
    f, ok := v.(float64)
    return f, ok
}

// RebindJSON round-trips args through JSON to rebind into a struct.
func RebindJSON(args map[string]any, dst any) error {
    b, err := json.Marshal(args)
    if err != nil {
        return Fail("cannot re-encode args: " + err.Error())
    }
    if err := json.Unmarshal(b, dst); err != nil {
        return Fail("invalid arguments: " + err.Error())
    }
    return nil
}
```

- [ ] **Step 5: Implement jsonrpc.go**

```go
// server/internal/mcp/jsonrpc.go
package mcp

import (
    "encoding/json"
    "net/http"
)

const protocolVersion = "2024-11-05"

type rpcRequest struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      any             `json:"id"`
    Method  string          `json:"method"`
    Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
    JSONRPC string    `json:"jsonrpc"`
    ID      any       `json:"id"`
    Result  any       `json:"result,omitempty"`
    Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
    Code    int    `json:"code"`
    Message string `json:"message"`
}

// MCPHandler returns a chi-compatible http.HandlerFunc for the MCP endpoint.
func MCPHandler(registry ToolRegistry) http.HandlerFunc {
    tools := buildToolsList(registry)
    return func(w http.ResponseWriter, r *http.Request) {
        var req rpcRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            writeRPC(w, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}})
            return
        }
        switch req.Method {
        case "initialize":
            writeRPC(w, rpcResponse{
                JSONRPC: "2.0", ID: req.ID,
                Result: map[string]any{
                    "protocolVersion": protocolVersion,
                    "capabilities":    map[string]any{"tools": map[string]any{}},
                    "serverInfo":      map[string]any{"name": "dashboard-tasks", "version": "1.0.0"},
                },
            })

        case "tools/list":
            writeRPC(w, rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": tools}})

        case "tools/call":
            var p struct {
                Name      string         `json:"name"`
                Arguments map[string]any `json:"arguments"`
            }
            if err := json.Unmarshal(req.Params, &p); err != nil {
                writeRPC(w, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32602, Message: "invalid params"}})
                return
            }
            def, ok := registry[p.Name]
            if !ok {
                writeRPC(w, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: "tool not found: " + p.Name}})
                return
            }
            // Scope check
            auth := AuthFromContext(r.Context())
            if auth == nil || !auth.Scopes[ToolScopeMap[p.Name]] {
                writeRPC(w, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32003, Message: "Insufficient scope: requires " + ToolScopeMap[p.Name]}})
                return
            }
            result, err := def.Handler(r.Context(), p.Arguments)
            if err != nil {
                msg := err.Error()
                writeRPC(w, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32003, Message: msg}})
                return
            }
            writeRPC(w, rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result})

        default:
            writeRPC(w, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: "method not found: " + req.Method}})
        }
    }
}

func writeRPC(w http.ResponseWriter, resp rpcResponse) {
    w.Header().Set("Content-Type", "application/json")
    _ = json.NewEncoder(w).Encode(resp)
}

// buildToolsList returns the MCP tools/list payload for all registered tools.
func buildToolsList(registry ToolRegistry) []map[string]any {
    out := make([]map[string]any, 0, len(registry))
    for _, def := range registry {
        out = append(out, map[string]any{
            "name":        def.Name,
            "description": def.Description,
            "inputSchema": def.InputSchema,
        })
    }
    return out
}
```

- [ ] **Step 6: Run tests to verify pass**

```bash
cd server && go test ./internal/mcp/... -v
```
Expected: PASS (auth scope tests pass)

- [ ] **Step 7: Verify build**

```bash
cd server && go build ./...
```
Expected: no errors

- [ ] **Step 8: Commit**

```bash
git add server/internal/mcp/
git commit -m "feat(mcp): JSON-RPC core + auth middleware + ToolRegistry"
```

---

## Task 3: Read tools (5 tools)

**Files:**
- Create: `server/internal/mcp/tools/read.go`

These tools mirror `server/mcp/tools/readTools.ts`. All require `tasks:read` scope.

- [ ] **Step 1: Write failing test for read tools registration**

```go
// server/internal/mcp/tools/read_test.go
package tools_test

import (
    "context"
    "testing"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
    "github.com/lx-wnk/agent-dashboard/server/internal/mcp"
    "github.com/lx-wnk/agent-dashboard/server/internal/mcp/tools"
    "github.com/stretchr/testify/require"
)

func TestRegisterReadTools(t *testing.T) {
    registry := mcp.ToolRegistry{}
    tools.RegisterReadTools(registry, nil, nil, nil, nil, nil)
    require.Contains(t, registry, "list_tasks")
    require.Contains(t, registry, "get_task")
    require.Contains(t, registry, "list_stage_runs")
    require.Contains(t, registry, "list_audit")
    require.Contains(t, registry, "list_permission_requests")
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd server && go test ./internal/mcp/tools/... -run TestRegisterReadTools -v
```
Expected: compile error

- [ ] **Step 3: Implement read.go**

```go
// server/internal/mcp/tools/read.go
package tools

import (
    "context"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
    "github.com/lx-wnk/agent-dashboard/server/internal/mcp"
)

// ReadDeps holds repos needed by the 5 read tools.
type ReadDeps struct {
    TaskRepo    repo.TaskRepo
    SRRepo      repo.StageRunRepo
    PermRepo    repo.PermissionRepo
    AuditRepo   repo.AuditRepo
}

func RegisterReadTools(registry mcp.ToolRegistry, d ReadDeps) {
    registry.Register(&mcp.ToolDef{
        Name:        "list_tasks",
        Description: "List all pipeline tasks, optionally filtered by stage",
        InputSchema: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "stage": map[string]any{"type": "string", "description": "Filter by pipeline stage"},
            },
        },
        Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
            auth := mcp.AuthFromContext(ctx)
            stage := mcp.OptionalString(args, "stage")
            var tasks []*repo.TaskRow
            var err error
            if stage != "" {
                tasks, err = d.TaskRepo.ListByStage(ctx, stage)
            } else {
                isAdmin := auth == nil || auth.UserID == ""
                userID := ""
                if auth != nil { userID = auth.UserID }
                tasks, err = d.TaskRepo.ListForUser(ctx, userID, isAdmin)
            }
            if err != nil {
                return nil, mcp.Fail("list_tasks: " + err.Error())
            }
            return mcp.OK(tasks)
        },
    })

    registry.Register(&mcp.ToolDef{
        Name:        "get_task",
        Description: "Get a task by UUID or slug",
        InputSchema: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "id_or_slug": map[string]any{"type": "string", "description": "Task UUID or slug"},
            },
            "required": []string{"id_or_slug"},
        },
        Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
            idOrSlug, err := mcp.StringArg(args, "id_or_slug")
            if err != nil { return nil, err }
            task, err := d.TaskRepo.GetByID(ctx, idOrSlug)
            if err != nil {
                task, err = d.TaskRepo.GetBySlug(ctx, idOrSlug)
            }
            if err != nil { return nil, mcp.Fail("Task not found: " + idOrSlug) }
            if denied := checkTaskAccess(ctx, task.UserID); denied != nil { return nil, denied }
            return mcp.OK(task)
        },
    })

    registry.Register(&mcp.ToolDef{
        Name:        "list_stage_runs",
        Description: "List stage runs for a task",
        InputSchema: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "task_id": map[string]any{"type": "string"},
            },
            "required": []string{"task_id"},
        },
        Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
            taskID, err := mcp.StringArg(args, "task_id")
            if err != nil { return nil, err }
            task, err := d.TaskRepo.GetByID(ctx, taskID)
            if err != nil { return nil, mcp.Fail("Task not found: " + taskID) }
            if denied := checkTaskAccess(ctx, task.UserID); denied != nil { return nil, denied }
            runs, err := d.SRRepo.ListForTask(ctx, taskID)
            if err != nil { return nil, mcp.Fail(err.Error()) }
            return mcp.OK(runs)
        },
    })

    registry.Register(&mcp.ToolDef{
        Name:        "list_audit",
        Description: "List audit log entries for a task",
        InputSchema: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "task_id": map[string]any{"type": "string"},
            },
            "required": []string{"task_id"},
        },
        Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
            taskID, err := mcp.StringArg(args, "task_id")
            if err != nil { return nil, err }
            task, err := d.TaskRepo.GetByID(ctx, taskID)
            if err != nil { return nil, mcp.Fail("Task not found: " + taskID) }
            if denied := checkTaskAccess(ctx, task.UserID); denied != nil { return nil, denied }
            entries, err := d.AuditRepo.ListForTask(ctx, taskID)
            if err != nil { return nil, mcp.Fail(err.Error()) }
            return mcp.OK(entries)
        },
    })

    registry.Register(&mcp.ToolDef{
        Name:        "list_permission_requests",
        Description: "List pending permission requests for a task",
        InputSchema: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "task_id": map[string]any{"type": "string"},
            },
            "required": []string{"task_id"},
        },
        Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
            taskID, err := mcp.StringArg(args, "task_id")
            if err != nil { return nil, err }
            task, err := d.TaskRepo.GetByID(ctx, taskID)
            if err != nil { return nil, mcp.Fail("Task not found: " + taskID) }
            if denied := checkTaskAccess(ctx, task.UserID); denied != nil { return nil, denied }
            runs, err := d.SRRepo.ListForTask(ctx, taskID)
            if err != nil { return nil, mcp.Fail(err.Error()) }
            var reqs []*repo.PermissionRequestRow
            for _, r := range runs {
                pr, err := d.PermRepo.ListPendingForRun(ctx, r.ID)
                if err != nil { return nil, mcp.Fail(err.Error()) }
                reqs = append(reqs, pr...)
            }
            if reqs == nil { reqs = []*repo.PermissionRequestRow{} }
            return mcp.OK(reqs)
        },
    })
}

// checkTaskAccess returns a Fail error if the caller's user ID doesn't match the task's user ID.
// Tasks with no user ID are accessible to all callers.
func checkTaskAccess(ctx context.Context, taskUserID *string) error {
    if taskUserID == nil { return nil }
    auth := mcp.AuthFromContext(ctx)
    if auth == nil || auth.UserID == "" { return nil }  // admin / no user filter
    if auth.UserID != *taskUserID { return mcp.Fail("Access denied: task belongs to a different user") }
    return nil
}
```

Note: The repo interfaces return `*ent.Task`, `*ent.StageRun`, etc. — use those directly. Replace `*repo.TaskRow` / `*repo.PermissionRequestRow` with the actual ent types that the existing repos return. Check `server/internal/db/repo/task_repo.go` and `permission_repo.go` for the exact return types. Also check `StageRunRepo` for a `ListForTask` method — add it if missing.

- [ ] **Step 4: Run tests to verify pass**

```bash
cd server && go test ./internal/mcp/... -v
```
Expected: PASS

- [ ] **Step 5: Build check**

```bash
cd server && go build ./...
```
Expected: clean

- [ ] **Step 6: Commit**

```bash
git add server/internal/mcp/tools/read.go server/internal/mcp/tools/read_test.go
git commit -m "feat(mcp): register 5 read tools (list_tasks, get_task, list_stage_runs, list_audit, list_permission_requests)"
```

---

## Task 4: Write tools (6 tools)

**Files:**
- Create: `server/internal/mcp/tools/write.go`

These mirror `server/mcp/tools/writeTools.ts` + `dependencyTools.ts`. All require `tasks:write` scope.

Tools: `create_task`, `update_task`, `delete_task`, `manage_task`, `add_dependency`, `remove_dependency`

- [ ] **Step 1: Write failing test for write tools registration**

```go
// server/internal/mcp/tools/write_test.go
package tools_test

import (
    "testing"
    "github.com/lx-wnk/agent-dashboard/server/internal/mcp"
    "github.com/lx-wnk/agent-dashboard/server/internal/mcp/tools"
    "github.com/stretchr/testify/require"
)

func TestRegisterWriteTools(t *testing.T) {
    registry := mcp.ToolRegistry{}
    tools.RegisterWriteTools(registry, tools.WriteDeps{})
    require.Contains(t, registry, "create_task")
    require.Contains(t, registry, "update_task")
    require.Contains(t, registry, "delete_task")
    require.Contains(t, registry, "manage_task")
    require.Contains(t, registry, "add_dependency")
    require.Contains(t, registry, "remove_dependency")
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd server && go test ./internal/mcp/tools/... -run TestRegisterWriteTools -v
```
Expected: compile error

- [ ] **Step 3: Implement write.go**

Implement `WriteDeps` struct and `RegisterWriteTools` function. For each tool:

**create_task**: accepts slug, title, cwd, description?, priority?, silverBullet?, metadata?, sourceBranch?, targetBranch?, parentTaskId?, maxIterations?, tokenBudget?, costBudgetCents?, template?, permissions[]?, inheritPermissions?

```go
// Validate slug against same pattern as rest of codebase: [a-z0-9][a-z0-9-]{0,63}
slugRE := regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
if !slugRE.MatchString(slug) { return nil, mcp.Fail(slugPatternMessage) }
```

Check for existing slug before create. Apply template if provided (call `PermRepo.ApplyTemplate`). Apply explicit permissions[] if provided (call `PermRepo.BulkGrant`). Apply inherit if `inheritPermissions=true && parentTaskId != "" && no explicit permissions`.

**update_task**: accepts id + optional fields (title, description, priority, silverBullet, maxIterations, tokenBudget, costBudgetCents, metadata). Build `UpdateTaskInput` from non-nil fields and call `TaskRepo.Update`. Call broadcast after.

**delete_task**: look up task, check access, call `TaskRepo.Delete`. Call broadcastDeleted.

**manage_task**: single tool with `action` discriminator:
- `grant_permissions`: validate permissions[], call ApplyTemplate + BulkGrant
- `revoke_permission`: call `PermRepo.DeleteByID`
- `list_permissions`: call `PermRepo.ListForTask` (with effective_only filter)
- `inherit_from_parent`: call `PermRepo.InheritFromParent(taskID, parentTaskID)`
- `set_metadata`: shallow-merge metadata patch → `TaskRepo.Update`
- `set_priority`: update priority/silverBullet → `TaskRepo.Update`
- `set_budget`: update tokenBudget/costBudgetCents/maxIterations → `TaskRepo.Update`

All actions call `AuditRepo.Append` and broadcast.

**add_dependency** / **remove_dependency**: call `DependencyRepo.Add` / `DependencyRepo.Remove`. Broadcast taskID after.

Check existing repo methods for exact signatures. The `PermRepo` interface in `server/internal/db/repo/permission_repo.go` may need additional methods:
- `ApplyTemplate(ctx, taskID, templateName string) error` (if not present, add it)
- `BulkGrant(ctx, taskID string, entries []GrantEntry) error` (if not present, add it)
- `InheritFromParent(ctx, taskID, parentTaskID string) error` (if not present, add it)
- `DeleteByID(ctx, permID string) (bool, error)` (if not present, add it)
- `ListForTask(ctx, taskID string, effectiveOnly bool) ([]*ent.TaskPermission, error)` (check if exists)

**Important**: Check `server/internal/db/repo/permission_repo.go` first and add any missing methods to the interface + implementation. Do NOT add them inside the mcp package.

The allowed tools block-list for `grant_permission` and `manage_task`→`grant_permissions` is defined in `server/internal/pipeline/spawner.go` as `allowedTools` map. Import the set from there or duplicate it in a shared location. To avoid circular imports, extract `AllowedTools` to `server/internal/pipeline/allowlist.go` (exported var) and import from there in the mcp tools.

- [ ] **Step 4: Run tests to verify pass**

```bash
cd server && go test ./internal/mcp/... -v
```
Expected: PASS

- [ ] **Step 5: Build check**

```bash
cd server && go build ./...
```
Expected: clean

- [ ] **Step 6: Commit**

```bash
git add server/internal/mcp/tools/write.go server/internal/mcp/tools/write_test.go
git commit -m "feat(mcp): register 6 write tools (create/update/delete task, manage_task, add/remove_dependency)"
```

---

## Task 5: Control tools + key tools (8 tools)

**Files:**
- Create: `server/internal/mcp/tools/control.go`
- Create: `server/internal/mcp/tools/keys.go`

These mirror `server/mcp/tools/controlTools.ts` and `keyTools.ts`.

- [ ] **Step 1: Write failing tests**

```go
// server/internal/mcp/tools/control_test.go
package tools_test

import (
    "testing"
    "github.com/lx-wnk/agent-dashboard/server/internal/mcp"
    "github.com/lx-wnk/agent-dashboard/server/internal/mcp/tools"
    "github.com/stretchr/testify/require"
)

func TestRegisterControlTools(t *testing.T) {
    registry := mcp.ToolRegistry{}
    tools.RegisterControlTools(registry, tools.ControlDeps{})
    require.Contains(t, registry, "progress_task")
    require.Contains(t, registry, "cancel_task")
    require.Contains(t, registry, "retry_task")
    require.Contains(t, registry, "grant_permission")
    require.Contains(t, registry, "resolve_permission_request")
}

func TestRegisterKeyTools(t *testing.T) {
    registry := mcp.ToolRegistry{}
    tools.RegisterKeyTools(registry, tools.KeyDeps{})
    require.Contains(t, registry, "list_api_keys")
    require.Contains(t, registry, "create_api_key")
    require.Contains(t, registry, "revoke_api_key")
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd server && go test ./internal/mcp/tools/... -run "TestRegisterControlTools|TestRegisterKeyTools" -v
```
Expected: compile error

- [ ] **Step 3: Implement control.go**

```go
// ControlDeps holds what control tools need.
type ControlDeps struct {
    TaskRepo    repo.TaskRepo
    SRRepo      repo.StageRunRepo
    PermRepo    repo.PermissionRepo
    AuditRepo   repo.AuditRepo
    Orchestrator OrchestratorIface   // reuse existing OrchestratorIface from api/tasks package
    Broadcast   func(taskID string)
}
```

Note: `OrchestratorIface` is defined in `server/internal/api/tasks/handler.go`. To avoid importing `api/tasks` from `mcp/tools` (which would create a layering violation), define a minimal local interface in `mcp/tools/control.go`:

```go
type ControlOrchestrator interface {
    ProgressTask(ctx context.Context, taskID string) (*ent.StageRun, error)
    ResumeFromUser(ctx context.Context, taskID string) error
    NotifyTaskTerminated(taskID, reason string)
}
```

**progress_task**: look up task → check access → `orch.ProgressTask(ctx, id)` → if nil, Fail("cannot progress") → broadcast → return {task, stageRun}

**cancel_task**: look up task → check terminal → DB transaction: `TaskRepo.Update(ctx, id, {CurrentStage: "cancelled"})` + `AuditRepo.Append(...)` → `orch.NotifyTaskTerminated(id, "cancelled")` → broadcast → return {task}

**retry_task**: look up task → get latest stage run → verify it's failed on current stage → `AuditRepo.Append` retry_requested → `orch.ProgressTask` → broadcast → return {task, stageRun}

**grant_permission**: look up task → check access → validate tool against `allowedTools` block-list → `PermRepo.Grant(ctx, taskID, tool, pattern)` → return permission row

**resolve_permission_request**: look up request → `PermRepo.ResolveRequest(ctx, requestID, outcome)` → if granted: `PermRepo.Grant(ctx, taskID, tool, pattern)` → `orch.ResumeFromUser(ctx, taskID)` → broadcast → return {resolved, resumed}

Check `PermRepo` interface for:
- `Grant(ctx, taskID, tool string, pattern *string) (*ent.TaskPermission, error)` — check/add
- `GetRequestByID(ctx, requestID string) (*ent.PermissionRequest, error)` — check/add
- `ResolveRequest(ctx, requestID, outcome string) (*ent.PermissionRequest, error)` — check/add

- [ ] **Step 4: Implement keys.go**

```go
// KeyDeps holds what key tools need.
type KeyDeps struct {
    ApiKeyRepo repo.ApiKeyRepo
}

func RegisterKeyTools(registry mcp.ToolRegistry, d KeyDeps) {
    // list_api_keys: d.ApiKeyRepo.List(ctx) — filter revoked if include_revoked=false
    // create_api_key: mcp.GenerateAPIToken() → mcp.HashToken(token) → d.ApiKeyRepo.Create(ctx, name, hash, scopes) → return {key, token}
    // revoke_api_key: d.ApiKeyRepo.GetByID(ctx, id) → d.ApiKeyRepo.Delete(ctx, id)
}
```

Note: `ApiKeyRepo.List` currently only returns active keys. For `include_revoked=true` support, check the `List` method signature. If it doesn't support `includeRevoked`, add `ListAll(ctx) ([]*ent.ApiKey, error)` to the interface.

- [ ] **Step 5: Run tests to verify pass**

```bash
cd server && go test ./internal/mcp/... -v
```
Expected: PASS

- [ ] **Step 6: Build check**

```bash
cd server && go build ./...
```
Expected: clean

- [ ] **Step 7: Commit**

```bash
git add server/internal/mcp/tools/control.go server/internal/mcp/tools/control_test.go server/internal/mcp/tools/keys.go server/internal/mcp/tools/keys_test.go
git commit -m "feat(mcp): register 8 control+key tools (progress/cancel/retry, grant_permission, resolve_permission_request, api key CRUD)"
```

---

## Task 6: BuildMCPServer + router wire + integration test

**Files:**
- Create: `server/internal/mcp/server.go`
- Modify: `server/internal/api/router.go`
- Modify: `server/cmd/serve/wire_gen.go`
- Create: `server/internal/mcp/mcp_test.go` (integration test)

- [ ] **Step 1: Implement server.go**

```go
// server/internal/mcp/server.go
package mcp

import (
    "net/http"
    "github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
    "github.com/lx-wnk/agent-dashboard/server/internal/mcp/tools"
)

// MCPDeps holds all dependencies needed to build the MCP handler.
type MCPDeps struct {
    TaskRepo    repo.TaskRepo
    SRRepo      repo.StageRunRepo
    PermRepo    repo.PermissionRepo
    AuditRepo   repo.AuditRepo
    CfgRepo     repo.PipelineConfigRepo
    DepRepo     repo.DependencyRepo
    ApiKeyRepo  repo.ApiKeyRepo
    Orchestrator tools.ControlOrchestrator
    Broadcast   func(taskID string)
    BroadcastDeleted func(taskID string)
}

// BuildMCPHandler constructs the full MCP JSON-RPC handler with all 19 tools registered.
func BuildMCPHandler(deps MCPDeps) http.Handler {
    registry := ToolRegistry{}

    tools.RegisterReadTools(registry, tools.ReadDeps{
        TaskRepo:  deps.TaskRepo,
        SRRepo:    deps.SRRepo,
        PermRepo:  deps.PermRepo,
        AuditRepo: deps.AuditRepo,
    })

    tools.RegisterWriteTools(registry, tools.WriteDeps{
        TaskRepo:         deps.TaskRepo,
        PermRepo:         deps.PermRepo,
        AuditRepo:        deps.AuditRepo,
        DepRepo:          deps.DepRepo,
        Broadcast:        deps.Broadcast,
        BroadcastDeleted: deps.BroadcastDeleted,
    })

    tools.RegisterControlTools(registry, tools.ControlDeps{
        TaskRepo:     deps.TaskRepo,
        SRRepo:       deps.SRRepo,
        PermRepo:     deps.PermRepo,
        AuditRepo:    deps.AuditRepo,
        Orchestrator: deps.Orchestrator,
        Broadcast:    deps.Broadcast,
    })

    tools.RegisterKeyTools(registry, tools.KeyDeps{
        ApiKeyRepo: deps.ApiKeyRepo,
    })

    return MCPHandler(registry)
}
```

- [ ] **Step 2: Wire into router**

In `server/internal/api/router.go`, add `MCPHandler http.Handler` to `RouterDeps`:

```go
type RouterDeps struct {
    // ... existing fields ...
    MCPHandler http.Handler
}
```

In `NewRouter`, mount MCP endpoint (protected by auth middleware for API keys):

```go
// In the route setup, before or alongside the tasks group:
if deps.MCPHandler != nil {
    // MCP uses its own Bearer auth (API keys), not JWT session auth.
    r.With(mcp.McpAuthMiddleware(deps.ApiKeyRepo)).Post("/api/mcp", deps.MCPHandler.ServeHTTP)
}
```

Note: import `mcp "github.com/lx-wnk/agent-dashboard/server/internal/mcp"`.

- [ ] **Step 3: Wire in wire_gen.go**

In `initializeServer`, after building orchestrator and task handler:

```go
mcpHandler := mcp.BuildMCPHandler(mcp.MCPDeps{
    TaskRepo:         repo.NewTaskRepo(entClient),
    SRRepo:           repo.NewStageRunRepo(entClient),
    PermRepo:         repo.NewPermissionRepo(entClient),
    AuditRepo:        repo.NewAuditRepo(entClient),
    DepRepo:          repo.NewDependencyRepo(entClient),
    ApiKeyRepo:       repo.NewApiKeyRepo(entClient),
    Orchestrator:     orch,
    Broadcast:        func(taskID string) { taskBroadcaster.Broadcast(sse.TaskEvent{Type: "task_changed", TaskID: taskID}) },
    BroadcastDeleted: func(taskID string) { taskBroadcaster.Broadcast(sse.TaskEvent{Type: "task_deleted", TaskID: taskID}) },
})
```

Add `MCPHandler: mcpHandler` to `provideRouterDeps` call / `RouterDeps`.

Also wire `McpAuthMiddleware` — the middleware needs `ApiKeyRepo`. Pass it from `RouterDeps.ApiKeyRepo` (already present).

- [ ] **Step 4: Write integration test**

```go
// server/internal/mcp/mcp_test.go
package mcp_test

import (
    "bytes"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/lx-wnk/agent-dashboard/server/internal/mcp"
    "github.com/stretchr/testify/require"
)

func TestMCPInitialize(t *testing.T) {
    registry := mcp.ToolRegistry{}
    handler := mcp.MCPHandler(registry)

    body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`
    req := httptest.NewRequest(http.MethodPost, "/api/mcp", bytes.NewBufferString(body))
    req.Header.Set("Content-Type", "application/json")
    w := httptest.NewRecorder()
    handler.ServeHTTP(w, req)

    require.Equal(t, http.StatusOK, w.Code)
    var resp map[string]any
    require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
    require.Equal(t, "2.0", resp["jsonrpc"])
    result := resp["result"].(map[string]any)
    require.Equal(t, "2024-11-05", result["protocolVersion"])
}

func TestMCPToolsList(t *testing.T) {
    registry := mcp.ToolRegistry{}
    // Register a fake tool
    registry.Register(&mcp.ToolDef{
        Name: "test_tool", Description: "A test tool",
        InputSchema: map[string]any{"type": "object"},
        Handler: func(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
            return mcp.OK(map[string]string{"hello": "world"})
        },
    })
    handler := mcp.MCPHandler(registry)

    body := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
    req := httptest.NewRequest(http.MethodPost, "/api/mcp", bytes.NewBufferString(body))
    w := httptest.NewRecorder()
    handler.ServeHTTP(w, req)

    require.Equal(t, http.StatusOK, w.Code)
    var resp map[string]any
    require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
    tools := resp["result"].(map[string]any)["tools"].([]any)
    require.Len(t, tools, 1)
    require.Equal(t, "test_tool", tools[0].(map[string]any)["name"])
}
```

- [ ] **Step 5: Run all tests**

```bash
cd server && go test ./... -v 2>&1 | tail -30
```
Expected: all 13+ packages PASS

- [ ] **Step 6: Build check**

```bash
cd server && go build ./...
```
Expected: clean

- [ ] **Step 7: Commit**

```bash
git add server/internal/mcp/server.go server/internal/mcp/mcp_test.go server/internal/api/router.go server/cmd/serve/wire_gen.go
git commit -m "feat(mcp): wire MCP endpoint at POST /api/mcp — 19 tools, 4 scope tiers"
```
