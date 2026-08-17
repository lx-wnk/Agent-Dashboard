# Spawn Dialog — Project Picker + Quick-Create Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let users pick (or quick-create) a project in the "New Agent" modal so the project's default folder hydrates `cwd` and the project's default spawner actually drives the spawn.

**Architecture:** Frontend adds a project + (conditional) folder select and an inline quick-create panel to `SpawnDialog.vue`, backed by a `useSpawnDialog` composable for hydration logic. Backend extends `SpawnManager.Spawn` to accept `spawnerId` and dispatches the spawner's command/args/env (claude or custom adapter) with the same env-merge rules as the pipeline path. Layering rule: `api/agents` does NOT import `pipeline/`; dispatch logic is copied locally (~50 lines).

**Tech Stack:** Vue 3 SFC + script-setup, Vitest + @vue/test-utils, Playwright (e2e), Go 1.26 (chi, ent, slog), `repo.SpawnerRepo` (ent-backed).

**Spec:** `docs/superpowers/specs/2026-05-22-spawn-dialog-project-picker-design.md`

---

## File Structure

### Files to create

- `src/composables/useSpawnDialog.ts` — pure hydration logic (project change → folders, cwd, model, spawnerId). Decoupled from Vue rendering so it can be unit-tested directly.
- `src/composables/__tests__/useSpawnDialog.test.ts` — Vitest unit tests for the composable.
- `src/components/QuickCreateProjectPanel.vue` — inline collapsible panel for creating a project + default folder. Emits `created(project)` on success.
- `src/components/QuickCreateProjectPanel.test.ts` — Vitest component test for the create + rollback flow.
- `src/components/SpawnDialog.test.ts` — Vitest component test for project selection flow.
- `e2e/spawn-with-project.spec.ts` — Playwright happy path.

### Files to modify

- `src/components/SpawnDialog.vue` — add Project select, conditional Folder select, mount `QuickCreateProjectPanel`, wire `useSpawnDialog`, extend payload with `spawnerId` + `projectId`.
- `server/internal/api/agents/spawn.go` — add `spawnerRepo repo.SpawnerRepo` to `SpawnManager`, accept `spawnerId`/`projectId` in body, dispatch claude/custom adapters with env merge + command allow-list re-check + model hydration. Add `SpawnerID` to `SpawnStatus`.
- `server/internal/api/agents/spawn_test.go` — add table-driven dispatch tests with a fake `SpawnerRepo`.
- `server/internal/api/router.go` — pass `deps.SpawnerRepo` to `NewSpawnManager`.

---

## Task 1: Backend — Plumb `spawnerRepo` through `SpawnManager` constructor

**Files:**
- Modify: `server/internal/api/agents/spawn.go:64-77` (`NewSpawnManager`), `:53-61` (`SpawnManager` struct)
- Modify: `server/internal/api/agents/spawn_test.go` (add nil-repo default test)
- Modify: `server/internal/api/router.go:253` (pass `deps.SpawnerRepo`)

- [ ] **Step 1: Write the failing test for constructor accepting nil repo**

Add to `server/internal/api/agents/spawn_test.go` (top of file imports already include `testing`):

```go
func TestNewSpawnManager_AcceptsNilRepo(t *testing.T) {
	m := NewSpawnManager(5, 60000, nil)
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
	if m.spawnerRepo != nil {
		t.Fatalf("expected nil spawnerRepo, got %v", m.spawnerRepo)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/api/agents/ -run TestNewSpawnManager_AcceptsNilRepo -v`
Expected: FAIL with `too many arguments in call to NewSpawnManager` (compile error).

- [ ] **Step 3: Update `SpawnManager` struct + constructor**

Change `server/internal/api/agents/spawn.go` lines 53-77 from:

```go
type SpawnManager struct {
	rateLimitMax    int
	rateLimitWindow time.Duration

	mu           sync.Mutex
	userAttempts map[string][]time.Time
	spawnStore   map[int]*SpawnStatus
}

func NewSpawnManager(maxSpawns int, windowMs int) *SpawnManager {
	if maxSpawns <= 0 {
		maxSpawns = 5
	}
	if windowMs <= 0 {
		windowMs = 60_000
	}
	return &SpawnManager{
		rateLimitMax:    maxSpawns,
		rateLimitWindow: time.Duration(windowMs) * time.Millisecond,
		userAttempts:    make(map[string][]time.Time),
		spawnStore:      make(map[int]*SpawnStatus),
	}
}
```

To:

```go
type SpawnManager struct {
	rateLimitMax    int
	rateLimitWindow time.Duration
	spawnerRepo     repo.SpawnerRepo

	mu           sync.Mutex
	userAttempts map[string][]time.Time
	spawnStore   map[int]*SpawnStatus
}

func NewSpawnManager(maxSpawns int, windowMs int, spawnerRepo repo.SpawnerRepo) *SpawnManager {
	if maxSpawns <= 0 {
		maxSpawns = 5
	}
	if windowMs <= 0 {
		windowMs = 60_000
	}
	return &SpawnManager{
		rateLimitMax:    maxSpawns,
		rateLimitWindow: time.Duration(windowMs) * time.Millisecond,
		spawnerRepo:     spawnerRepo,
		userAttempts:    make(map[string][]time.Time),
		spawnStore:      make(map[int]*SpawnStatus),
	}
}
```

Add the import at the top of `spawn.go`:

```go
"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
```

- [ ] **Step 4: Update other test callers + router caller**

In `server/internal/api/agents/spawn_test.go`, replace every existing `NewSpawnManager(a, b)` call with `NewSpawnManager(a, b, nil)`. Use grep first:

```bash
grep -n "NewSpawnManager(" server/internal/api/agents/spawn_test.go
```

Then edit each line so the call has a third `nil` argument.

In `server/internal/api/router.go` line 253, change:

```go
spawnMgr := agents.NewSpawnManager(deps.Config.SpawnRateLimit, deps.Config.SpawnRateWindowMs)
```

To:

```go
spawnMgr := agents.NewSpawnManager(deps.Config.SpawnRateLimit, deps.Config.SpawnRateWindowMs, deps.SpawnerRepo)
```

- [ ] **Step 5: Run all agents tests to verify they pass**

Run: `cd server && go test ./internal/api/agents/ -v`
Expected: PASS for all tests including the new `TestNewSpawnManager_AcceptsNilRepo`.

- [ ] **Step 6: Run server build to verify router compiles**

Run: `cd server && go build ./...`
Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add server/internal/api/agents/spawn.go \
        server/internal/api/agents/spawn_test.go \
        server/internal/api/router.go
git commit -m "refactor(agents): plumb SpawnerRepo into SpawnManager constructor"
```

---

## Task 2: Backend — Reject unsupported adapters + lookup spawner

**Files:**
- Modify: `server/internal/api/agents/spawn.go` (extend `Spawn` body parsing)
- Modify: `server/internal/api/agents/spawn_test.go` (new dispatch tests)

- [ ] **Step 1: Write failing test for `spawner not found`**

Add to `server/internal/api/agents/spawn_test.go`:

```go
import (
	"context"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
)

type fakeSpawnerRepo struct {
	byID map[string]*ent.Spawner
}

func (f *fakeSpawnerRepo) GetByID(ctx context.Context, id string) (*ent.Spawner, error) {
	if s, ok := f.byID[id]; ok {
		return s, nil
	}
	return nil, &ent.NotFoundError{}
}

// Stub the other SpawnerRepo methods (unused in spawn tests). The fake satisfies the
// interface by embedding nil for unused methods; if the interface adds methods, add
// them here as no-op returns.
func (f *fakeSpawnerRepo) List(ctx context.Context) ([]*ent.Spawner, error) { return nil, nil }
func (f *fakeSpawnerRepo) GetBySlug(ctx context.Context, slug string) (*ent.Spawner, error) {
	return nil, &ent.NotFoundError{}
}
func (f *fakeSpawnerRepo) Create(ctx context.Context, _ repo.CreateSpawnerInput) (*ent.Spawner, error) {
	return nil, nil
}
func (f *fakeSpawnerRepo) Update(ctx context.Context, _ string, _ repo.UpdateSpawnerInput) (*ent.Spawner, error) {
	return nil, nil
}
func (f *fakeSpawnerRepo) Delete(ctx context.Context, _ string) error { return nil }

func TestSpawn_UnknownSpawnerID_Returns400(t *testing.T) {
	m := NewSpawnManager(5, 60000, &fakeSpawnerRepo{byID: map[string]*ent.Spawner{}})
	_, err := m.Spawn("u1", map[string]any{
		"prompt":    "do thing",
		"cwd":      os.TempDir(),
		"spawnerId": "spwn_missing",
	})
	if err == nil || !strings.Contains(err.Error(), "spawner not found") {
		t.Fatalf("expected 'spawner not found' error, got %v", err)
	}
}
```

> **Note:** `repo.CreateSpawnerInput` / `UpdateSpawnerInput` must match what `repo.SpawnerRepo` declares. If the actual interface in `server/internal/db/repo/spawner_repo.go` uses different method names, mirror those exactly. Check with `grep "^func.*SpawnerRepo\|^	[A-Z].*(ctx" server/internal/db/repo/spawner_repo.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/api/agents/ -run TestSpawn_UnknownSpawnerID -v`
Expected: FAIL — either compile error (the lookup code is missing) or the test fails because cwd validation passes and the spawn falls through to running a real `claude` binary.

- [ ] **Step 3: Implement spawner lookup + adapter rejection**

In `server/internal/api/agents/spawn.go`, inside `Spawn(sub, body)` immediately after the `resumeSessionID` validation block (around line 141), insert:

```go
// Resolve spawner if provided.
var spawnerRow *ent.Spawner
if spawnerID, ok := body["spawnerId"].(string); ok && spawnerID != "" {
	if m.spawnerRepo == nil {
		return 0, fmt.Errorf("spawner not configured")
	}
	row, err := m.spawnerRepo.GetByID(context.Background(), spawnerID)
	if err != nil {
		if ent.IsNotFound(err) {
			return 0, fmt.Errorf("spawner not found")
		}
		return 0, fmt.Errorf("spawner lookup failed: %w", err)
	}
	switch row.AdapterType {
	case "ollama", "openai":
		return 0, fmt.Errorf("adapter %s not supported for user-initiated spawns; use pipeline tasks instead", row.AdapterType)
	}
	spawnerRow = row
}
_ = spawnerRow // used by next task
```

Add the imports at the top of `spawn.go`:

```go
"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
```

- [ ] **Step 4: Run lookup test to verify it passes**

Run: `cd server && go test ./internal/api/agents/ -run TestSpawn_UnknownSpawnerID -v`
Expected: PASS.

- [ ] **Step 5: Add adapter-rejection test cases**

Add to `spawn_test.go`:

```go
func TestSpawn_OllamaAdapter_Rejected(t *testing.T) {
	row := &ent.Spawner{ID: "spwn_o", AdapterType: "ollama", Command: "claude"}
	m := NewSpawnManager(5, 60000, &fakeSpawnerRepo{byID: map[string]*ent.Spawner{"spwn_o": row}})
	_, err := m.Spawn("u1", map[string]any{
		"prompt":    "do thing",
		"cwd":      os.TempDir(),
		"spawnerId": "spwn_o",
	})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected adapter-not-supported error, got %v", err)
	}
}

func TestSpawn_OpenAIAdapter_Rejected(t *testing.T) {
	row := &ent.Spawner{ID: "spwn_x", AdapterType: "openai", Command: "claude"}
	m := NewSpawnManager(5, 60000, &fakeSpawnerRepo{byID: map[string]*ent.Spawner{"spwn_x": row}})
	_, err := m.Spawn("u1", map[string]any{
		"prompt":    "do thing",
		"cwd":      os.TempDir(),
		"spawnerId": "spwn_x",
	})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected adapter-not-supported error, got %v", err)
	}
}
```

- [ ] **Step 6: Run all agents tests**

Run: `cd server && go test ./internal/api/agents/ -v`
Expected: PASS for all tests.

- [ ] **Step 7: Commit**

```bash
git add server/internal/api/agents/spawn.go \
        server/internal/api/agents/spawn_test.go
git commit -m "feat(agents): lookup spawner and reject unsupported adapters"
```

---

## Task 3: Backend — Dispatch claude + custom adapter, env merge, model hydration

**Files:**
- Modify: `server/internal/api/agents/spawn.go` (command + args + env resolution)
- Modify: `server/internal/api/agents/spawn_test.go` (dispatch + env merge tests)

- [ ] **Step 1: Write failing test for claude-adapter modelOverride hydration**

Append to `spawn_test.go`. The plan does NOT actually exec; we test the *command construction* indirectly by replacing the exec hook.

First, refactor `Spawn` to use an injectable exec function. Add this near the top of `spawn.go` (after the `var` block around line 32):

```go
// execStart is the seam used by tests to intercept spawn args without
// actually launching a process. Production uses cmd.Start directly.
var execStart = func(cmd *exec.Cmd) error { return cmd.Start() }
```

Then in `Spawn` replace `if err := cmd.Start(); err != nil {` with `if err := execStart(cmd); err != nil {`.

Now add the test:

```go
func TestSpawn_ClaudeAdapter_HydratesModelFromOverride(t *testing.T) {
	override := "claude-opus-4-7"
	row := &ent.Spawner{
		ID:            "spwn_c",
		AdapterType:   "claude",
		Command:       "claude",
		Args:          []string{},
		Env:           map[string]string{},
		ModelOverride: &override,
	}
	repo := &fakeSpawnerRepo{byID: map[string]*ent.Spawner{"spwn_c": row}}

	var capturedCmd *exec.Cmd
	origExec := execStart
	execStart = func(cmd *exec.Cmd) error {
		capturedCmd = cmd
		// Pretend the process started with a fake pid; the manager only reads cmd.Process.Pid.
		// Use a real (but already-exited) helper: launch /bin/true so Process is populated.
		return exec.Command("/bin/true").Start()
	}
	t.Cleanup(func() { execStart = origExec })

	m := NewSpawnManager(5, 60000, repo)
	_, err := m.Spawn("u1", map[string]any{
		"prompt":    "do thing",
		"cwd":      os.TempDir(),
		"spawnerId": "spwn_c",
		// no "model" key in body
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedCmd == nil {
		t.Fatal("execStart not invoked")
	}
	if !containsConsecutive(capturedCmd.Args, "--model", override) {
		t.Fatalf("expected --model %s in args, got %v", override, capturedCmd.Args)
	}
}

// containsConsecutive returns true if args contains a followed by b.
func containsConsecutive(args []string, a, b string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == a && args[i+1] == b {
			return true
		}
	}
	return false
}
```

> **Note on `execStart` workaround:** `cmd.Process.Pid` is normally populated by `cmd.Start()`. In the test we replace the real call with a side-channel start of `/bin/true` so the helper's Process exists; the captured `cmd` still has the constructed `cmd.Args` we want to assert on. The original `cmd` does not run because we never call `cmd.Start()` on it.

> **Side effect:** the test launches `/bin/true` once per dispatch test. Acceptable in unit tests; not in CI loops. Reaping happens via the goroutine that wait()s in the existing code path — it will harmlessly observe the bogus PID's stderr pipe close.

> **If `/bin/true` is unavailable** (some minimal containers), substitute `/bin/sh -c ":"`.

- [ ] **Step 2: Run new test — expect FAIL**

Run: `cd server && go test ./internal/api/agents/ -run TestSpawn_ClaudeAdapter_HydratesModelFromOverride -v`
Expected: FAIL — `--model claude-opus-4-7` not in args because dispatch logic is missing.

- [ ] **Step 3: Implement dispatch (claude + custom)**

In `server/internal/api/agents/spawn.go`, replace the block that constructs `args` (currently lines 147-157) with the new dispatch-aware construction. Replace:

```go
var args []string
if resumeSessionID != "" {
	args = append(args, "--resume", resumeSessionID)
}
args = append(args, "-p", prompt)
if model != "" {
	args = append(args, "--model", model)
}
if systemPrompt != "" {
	args = append(args, "--system-prompt", systemPrompt)
}
```

With:

```go
// Resolve command + base args.
binary := claudeBin
var spawnerArgs []string
if spawnerRow != nil {
	if !spawners.ValidateCommand(spawnerRow.Command) {
		return 0, fmt.Errorf("spawner command not permitted")
	}
	if spawnerRow.AdapterType == "custom" {
		binary = spawnerRow.Command
	}
	spawnerArgs = append(spawnerArgs, spawnerRow.Args...)

	// Hydrate model from override when caller didn't supply one.
	if model == "" && spawnerRow.ModelOverride != nil && *spawnerRow.ModelOverride != "" {
		model = *spawnerRow.ModelOverride
	}
}

var canonicalArgs []string
if resumeSessionID != "" {
	canonicalArgs = append(canonicalArgs, "--resume", resumeSessionID)
}
canonicalArgs = append(canonicalArgs, "-p", prompt)
if model != "" {
	canonicalArgs = append(canonicalArgs, "--model", model)
}
if systemPrompt != "" {
	canonicalArgs = append(canonicalArgs, "--system-prompt", systemPrompt)
}

// Order: spawner args first, canonical args last so user-supplied flags win.
args := append(spawnerArgs, canonicalArgs...)
```

Then further down where `cmd := exec.Command(claudeBin, args...)` is called, replace `claudeBin` with `binary`:

```go
cmd := exec.Command(binary, args...)
```

Add the import:

```go
"github.com/lx-wnk/agent-dashboard/server/internal/api/spawners"
```

- [ ] **Step 4: Run test to verify pass**

Run: `cd server && go test ./internal/api/agents/ -run TestSpawn_ClaudeAdapter_HydratesModelFromOverride -v`
Expected: PASS.

- [ ] **Step 5: Add body-model-wins test**

Append to `spawn_test.go`:

```go
func TestSpawn_BodyModelOverridesSpawnerModelOverride(t *testing.T) {
	override := "claude-opus-4-7"
	row := &ent.Spawner{
		ID: "spwn_c", AdapterType: "claude", Command: "claude",
		ModelOverride: &override,
	}
	repo := &fakeSpawnerRepo{byID: map[string]*ent.Spawner{"spwn_c": row}}

	var capturedCmd *exec.Cmd
	origExec := execStart
	execStart = func(cmd *exec.Cmd) error {
		capturedCmd = cmd
		return exec.Command("/bin/true").Start()
	}
	t.Cleanup(func() { execStart = origExec })

	m := NewSpawnManager(5, 60000, repo)
	_, err := m.Spawn("u1", map[string]any{
		"prompt":    "do thing",
		"cwd":      os.TempDir(),
		"spawnerId": "spwn_c",
		"model":    "claude-sonnet-4-6", // body wins
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsConsecutive(capturedCmd.Args, "--model", "claude-sonnet-4-6") {
		t.Fatalf("expected body model to win, got %v", capturedCmd.Args)
	}
	if containsConsecutive(capturedCmd.Args, "--model", override) {
		t.Fatalf("override should NOT appear when body model is set, got %v", capturedCmd.Args)
	}
}
```

- [ ] **Step 6: Add custom-adapter dispatch test**

Append to `spawn_test.go`:

```go
func TestSpawn_CustomAdapter_UsesSpawnerCommand(t *testing.T) {
	// Permit our test command via env var.
	t.Setenv("DASHBOARD_SPAWNER_ALLOWED_COMMANDS", "npx")

	row := &ent.Spawner{
		ID: "spwn_x", AdapterType: "custom", Command: "npx",
		Args: []string{"--", "claude-clone"},
	}
	repo := &fakeSpawnerRepo{byID: map[string]*ent.Spawner{"spwn_x": row}}

	var capturedCmd *exec.Cmd
	origExec := execStart
	execStart = func(cmd *exec.Cmd) error {
		capturedCmd = cmd
		return exec.Command("/bin/true").Start()
	}
	t.Cleanup(func() { execStart = origExec })

	m := NewSpawnManager(5, 60000, repo)
	_, err := m.Spawn("u1", map[string]any{
		"prompt":    "do thing",
		"cwd":      os.TempDir(),
		"spawnerId": "spwn_x",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedCmd.Path != "npx" && !strings.HasSuffix(capturedCmd.Path, "/npx") {
		t.Fatalf("expected npx as command, got %s", capturedCmd.Path)
	}
	// spawner args must precede canonical args (-p, --model, …)
	if !containsConsecutive(capturedCmd.Args, "--", "claude-clone") {
		t.Fatalf("expected '-- claude-clone' from spawner.Args, got %v", capturedCmd.Args)
	}
}

func TestSpawn_CustomAdapter_DisallowedCommandRejected(t *testing.T) {
	t.Setenv("DASHBOARD_SPAWNER_ALLOWED_COMMANDS", "") // empty
	row := &ent.Spawner{
		ID: "spwn_bad", AdapterType: "custom", Command: "rm",
	}
	repo := &fakeSpawnerRepo{byID: map[string]*ent.Spawner{"spwn_bad": row}}
	m := NewSpawnManager(5, 60000, repo)
	_, err := m.Spawn("u1", map[string]any{
		"prompt":    "do thing",
		"cwd":      os.TempDir(),
		"spawnerId": "spwn_bad",
	})
	if err == nil || !strings.Contains(err.Error(), "not permitted") {
		t.Fatalf("expected 'not permitted' error, got %v", err)
	}
}
```

- [ ] **Step 7: Run all agents tests**

Run: `cd server && go test ./internal/api/agents/ -v`
Expected: PASS for all tests.

- [ ] **Step 8: Implement env merge**

Below the `args` construction but above `cmd := exec.Command(binary, args...)`, build a merged environment. Replace:

```go
cmd := exec.Command(binary, args...)
cmd.Dir = cwd
```

With:

```go
cmd := exec.Command(binary, args...)
cmd.Dir = cwd
cmd.Env = mergeEnv(spawnerRow)
```

Then add a helper at the bottom of `spawn.go`:

```go
// mergeEnv builds the child process env per ADR-0003:
//   1. start with os.Environ()
//   2. overlay spawner.Env
//   3. dashboard-controlled vars (DASHBOARD_*, CLAUDE_*) always win
//   4. strip DASHBOARD_JWT_SECRET and DASHBOARD_HOOKS_SECRET
// The "dashboard vars always win" step is implemented by re-applying every
// os.Environ entry whose key starts with DASHBOARD_ or CLAUDE_ AFTER the
// spawner overlay.
func mergeEnv(s *ent.Spawner) []string {
	// 1. start with os.Environ()
	merged := map[string]string{}
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 {
			merged[kv[:i]] = kv[i+1:]
		}
	}
	// 2. overlay spawner.Env
	if s != nil {
		for k, v := range s.Env {
			merged[k] = v
		}
	}
	// 3. dashboard vars always win — re-overlay from os.Environ
	for _, kv := range os.Environ() {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			continue
		}
		k := kv[:i]
		if strings.HasPrefix(k, "DASHBOARD_") || strings.HasPrefix(k, "CLAUDE_") {
			merged[k] = kv[i+1:]
		}
	}
	// 4. strip secrets
	delete(merged, "DASHBOARD_JWT_SECRET")
	delete(merged, "DASHBOARD_HOOKS_SECRET")
	out := make([]string, 0, len(merged))
	for k, v := range merged {
		out = append(out, k+"="+v)
	}
	return out
}
```

- [ ] **Step 9: Add env-merge tests**

Append to `spawn_test.go`:

```go
func TestSpawn_EnvMerge_DashboardWins(t *testing.T) {
	t.Setenv("DASHBOARD_MCP_TOKEN", "from-dashboard")
	t.Setenv("DASHBOARD_SPAWNER_ALLOWED_COMMANDS", "claude")

	row := &ent.Spawner{
		ID: "spwn_e", AdapterType: "claude", Command: "claude",
		Env: map[string]string{
			"DASHBOARD_MCP_TOKEN": "from-spawner", // must lose
			"MY_CUSTOM":            "hello",         // must survive
		},
	}
	repo := &fakeSpawnerRepo{byID: map[string]*ent.Spawner{"spwn_e": row}}

	var capturedCmd *exec.Cmd
	origExec := execStart
	execStart = func(cmd *exec.Cmd) error {
		capturedCmd = cmd
		return exec.Command("/bin/true").Start()
	}
	t.Cleanup(func() { execStart = origExec })

	m := NewSpawnManager(5, 60000, repo)
	if _, err := m.Spawn("u1", map[string]any{
		"prompt":    "do thing",
		"cwd":      os.TempDir(),
		"spawnerId": "spwn_e",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	gotDashboard := envValue(capturedCmd.Env, "DASHBOARD_MCP_TOKEN")
	if gotDashboard != "from-dashboard" {
		t.Fatalf("dashboard env must win: got %q", gotDashboard)
	}
	if envValue(capturedCmd.Env, "MY_CUSTOM") != "hello" {
		t.Fatalf("spawner env must survive: got %q", envValue(capturedCmd.Env, "MY_CUSTOM"))
	}
}

func TestSpawn_EnvMerge_SecretsStripped(t *testing.T) {
	t.Setenv("DASHBOARD_JWT_SECRET", "super-secret")
	t.Setenv("DASHBOARD_HOOKS_SECRET", "also-secret")
	t.Setenv("DASHBOARD_SPAWNER_ALLOWED_COMMANDS", "claude")

	row := &ent.Spawner{ID: "spwn_s", AdapterType: "claude", Command: "claude"}
	repo := &fakeSpawnerRepo{byID: map[string]*ent.Spawner{"spwn_s": row}}

	var capturedCmd *exec.Cmd
	origExec := execStart
	execStart = func(cmd *exec.Cmd) error {
		capturedCmd = cmd
		return exec.Command("/bin/true").Start()
	}
	t.Cleanup(func() { execStart = origExec })

	m := NewSpawnManager(5, 60000, repo)
	if _, err := m.Spawn("u1", map[string]any{
		"prompt":    "do thing",
		"cwd":      os.TempDir(),
		"spawnerId": "spwn_s",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if envValue(capturedCmd.Env, "DASHBOARD_JWT_SECRET") != "" {
		t.Fatalf("JWT secret must be stripped")
	}
	if envValue(capturedCmd.Env, "DASHBOARD_HOOKS_SECRET") != "" {
		t.Fatalf("hooks secret must be stripped")
	}
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return kv[len(prefix):]
		}
	}
	return ""
}
```

- [ ] **Step 10: Run all agents tests**

Run: `cd server && go test ./internal/api/agents/ -v`
Expected: PASS for all tests including the env tests.

- [ ] **Step 11: Add `SpawnerID` to `SpawnStatus`**

In `server/internal/api/agents/spawn.go` change the `SpawnStatus` struct from:

```go
type SpawnStatus struct {
	PID       int    `json:"pid"`
	Status    string `json:"status"`
	ExitCode  *int   `json:"exitCode"`
	Stderr    string `json:"stderr"`
	StartedAt string `json:"startedAt"`
	Prompt    string `json:"prompt"`
	Cwd       string `json:"cwd"`
}
```

To:

```go
type SpawnStatus struct {
	PID       int    `json:"pid"`
	Status    string `json:"status"`
	ExitCode  *int   `json:"exitCode"`
	Stderr    string `json:"stderr"`
	StartedAt string `json:"startedAt"`
	Prompt    string `json:"prompt"`
	Cwd       string `json:"cwd"`
	SpawnerID string `json:"spawnerId,omitempty"`
}
```

Where `SpawnStatus` is constructed (currently around line 187), set `SpawnerID`:

```go
status := &SpawnStatus{
	PID:       pid,
	Status:    "running",
	StartedAt: time.Now().UTC().Format(time.RFC3339),
	Prompt:    prompt[:min(len(prompt), 200)],
	Cwd:       cwd,
}
if spawnerRow != nil {
	status.SpawnerID = spawnerRow.ID
}
```

Also log projectId for observability — immediately after the spawner block in `Spawn()`:

```go
projectID, _ := body["projectId"].(string)
if projectID != "" {
	slog.Info("spawn: projectId attached", "projectId", projectID, "spawnerId", spawnerIDValue(spawnerRow))
}
```

Add the helper at the bottom of `spawn.go`:

```go
func spawnerIDValue(s *ent.Spawner) string {
	if s == nil {
		return ""
	}
	return s.ID
}
```

- [ ] **Step 12: Run full agents test suite + build**

Run: `cd server && go test ./internal/api/agents/ -v && go build ./...`
Expected: PASS for all tests; build succeeds.

- [ ] **Step 13: Run server lint + race tests**

Run: `cd server && task test`
Expected: PASS.

- [ ] **Step 14: Commit**

```bash
git add server/internal/api/agents/spawn.go \
        server/internal/api/agents/spawn_test.go
git commit -m "feat(agents): dispatch claude/custom spawner with env merge and model hydration"
```

---

## Task 4: Frontend — `useSpawnDialog` composable

**Files:**
- Create: `src/composables/useSpawnDialog.ts`
- Create: `src/composables/__tests__/useSpawnDialog.test.ts`

- [ ] **Step 1: Write failing test for hydration on project selection**

Create `src/composables/__tests__/useSpawnDialog.test.ts`:

```typescript
import type { Project, ProjectFolder, Spawner } from '../../types'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useSpawnDialog } from '../useSpawnDialog'

const sampleProject: Project = {
  id: 'prj_a',
  slug: 'alpha',
  name: 'Alpha',
  defaultSpawnerId: 'spwn_a',
  createdAt: '',
  updatedAt: '',
}

const sampleSpawner: Spawner = {
  id: 'spwn_a',
  name: 'Claude (Opus)',
  slug: 'claude-opus',
  command: 'claude',
  args: [],
  env: {},
  adapterType: 'claude',
  adapterConfig: {},
  modelOverride: 'claude-opus-4-7',
  builtIn: false,
  createdAt: '',
  updatedAt: '',
}

const singleFolder: ProjectFolder[] = [
  { id: 'fld_a', projectId: 'prj_a', path: '/home/u/alpha', isDefault: true, createdAt: '' },
]

const multiFolders: ProjectFolder[] = [
  { id: 'fld_a', projectId: 'prj_a', path: '/home/u/alpha-default', isDefault: true, createdAt: '' },
  { id: 'fld_b', projectId: 'prj_a', path: '/home/u/alpha-experimental', isDefault: false, createdAt: '' },
]

describe('useSpawnDialog', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('selecting a project with one folder hydrates cwd and model', async () => {
    const fetchFolders = vi.fn().mockResolvedValue(singleFolder)
    const lookupSpawner = vi.fn().mockReturnValue(sampleSpawner)

    const d = useSpawnDialog({ fetchFolders, lookupSpawner })
    await d.selectProject(sampleProject)

    expect(d.cwd.value).toBe('/home/u/alpha')
    expect(d.model.value).toBe('claude-opus-4-7')
    expect(d.spawnerId.value).toBe('spwn_a')
    expect(d.folders.value).toEqual(singleFolder)
    expect(d.selectedFolderId.value).toBe('fld_a')
  })

  it('selecting a project with multiple folders defaults to isDefault folder', async () => {
    const fetchFolders = vi.fn().mockResolvedValue(multiFolders)
    const lookupSpawner = vi.fn().mockReturnValue(sampleSpawner)

    const d = useSpawnDialog({ fetchFolders, lookupSpawner })
    await d.selectProject(sampleProject)

    expect(d.cwd.value).toBe('/home/u/alpha-default')
    expect(d.selectedFolderId.value).toBe('fld_a')
  })

  it('changing folder updates cwd', async () => {
    const fetchFolders = vi.fn().mockResolvedValue(multiFolders)
    const lookupSpawner = vi.fn().mockReturnValue(sampleSpawner)

    const d = useSpawnDialog({ fetchFolders, lookupSpawner })
    await d.selectProject(sampleProject)
    d.selectFolder('fld_b')

    expect(d.cwd.value).toBe('/home/u/alpha-experimental')
  })

  it('clearing project resets cwd, model, spawnerId', async () => {
    const fetchFolders = vi.fn().mockResolvedValue(singleFolder)
    const lookupSpawner = vi.fn().mockReturnValue(sampleSpawner)

    const d = useSpawnDialog({ fetchFolders, lookupSpawner })
    await d.selectProject(sampleProject)
    d.clearProject()

    expect(d.cwd.value).toBe('')
    expect(d.model.value).toBe('')
    expect(d.spawnerId.value).toBeNull()
    expect(d.folders.value).toEqual([])
  })

  it('project without defaultSpawnerId leaves model and spawnerId empty', async () => {
    const fetchFolders = vi.fn().mockResolvedValue(singleFolder)
    const lookupSpawner = vi.fn().mockReturnValue(undefined)

    const d = useSpawnDialog({ fetchFolders, lookupSpawner })
    await d.selectProject({ ...sampleProject, defaultSpawnerId: null })

    expect(d.model.value).toBe('')
    expect(d.spawnerId.value).toBeNull()
  })

  it('project with spawner but no modelOverride keeps model empty (Auto)', async () => {
    const fetchFolders = vi.fn().mockResolvedValue(singleFolder)
    const noOverride: Spawner = { ...sampleSpawner, modelOverride: undefined }
    const lookupSpawner = vi.fn().mockReturnValue(noOverride)

    const d = useSpawnDialog({ fetchFolders, lookupSpawner })
    await d.selectProject(sampleProject)

    expect(d.model.value).toBe('')
    expect(d.spawnerId.value).toBe('spwn_a')
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm vitest run src/composables/__tests__/useSpawnDialog.test.ts`
Expected: FAIL with `Cannot find module '../useSpawnDialog'`.

- [ ] **Step 3: Implement `useSpawnDialog`**

Create `src/composables/useSpawnDialog.ts`:

```typescript
import type { Project, ProjectFolder, Spawner } from '../types'
import { ref, shallowRef } from 'vue'

export interface UseSpawnDialogDeps {
  /** Returns the project's folder list. Caller decides whether to embed or fetch. */
  fetchFolders: (projectId: string) => Promise<ProjectFolder[]>
  /** Looks up a spawner by id from a local cache (e.g. useSpawners()). */
  lookupSpawner: (spawnerId: string) => Spawner | undefined
}

/**
 * State + actions for the project/folder/spawner hydration flow inside
 * the SpawnDialog modal. Decoupled from the SFC so it can be unit-tested
 * without rendering Vue.
 */
export function useSpawnDialog(deps: UseSpawnDialogDeps) {
  const project = shallowRef<Project | null>(null)
  const folders = shallowRef<ProjectFolder[]>([])
  const selectedFolderId = ref<string | null>(null)
  const cwd = ref('')
  const model = ref('')
  const spawnerId = ref<string | null>(null)

  async function selectProject(p: Project): Promise<void> {
    project.value = p
    const list = p.folders ?? await deps.fetchFolders(p.id)
    folders.value = list

    const defaultFolder = list.find(f => f.isDefault) ?? list[0] ?? null
    selectedFolderId.value = defaultFolder?.id ?? null
    cwd.value = defaultFolder?.path ?? ''

    if (p.defaultSpawnerId) {
      const sp = deps.lookupSpawner(p.defaultSpawnerId)
      spawnerId.value = sp?.id ?? null
      model.value = sp?.modelOverride ?? ''
    }
    else {
      spawnerId.value = null
      model.value = ''
    }
  }

  function selectFolder(folderId: string): void {
    const f = folders.value.find(x => x.id === folderId)
    if (!f)
      return
    selectedFolderId.value = folderId
    cwd.value = f.path
  }

  function clearProject(): void {
    project.value = null
    folders.value = []
    selectedFolderId.value = null
    cwd.value = ''
    model.value = ''
    spawnerId.value = null
  }

  return {
    project,
    folders,
    selectedFolderId,
    cwd,
    model,
    spawnerId,
    selectProject,
    selectFolder,
    clearProject,
  }
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `pnpm vitest run src/composables/__tests__/useSpawnDialog.test.ts`
Expected: PASS for all 6 cases.

- [ ] **Step 5: Commit**

```bash
git add src/composables/useSpawnDialog.ts \
        src/composables/__tests__/useSpawnDialog.test.ts
git commit -m "feat(spawn-dialog): add useSpawnDialog composable for project hydration"
```

---

## Task 5: Frontend — `QuickCreateProjectPanel` component

**Files:**
- Create: `src/components/QuickCreateProjectPanel.vue`
- Create: `src/components/QuickCreateProjectPanel.test.ts`

- [ ] **Step 1: Write failing component test**

Create `src/components/QuickCreateProjectPanel.test.ts`:

```typescript
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import QuickCreateProjectPanel from './QuickCreateProjectPanel.vue'

const sampleProject = {
  id: 'prj_new',
  slug: 'new-thing',
  name: 'New Thing',
  defaultSpawnerId: 'spwn_a',
  createdAt: '',
  updatedAt: '',
}
const sampleFolder = {
  id: 'fld_new',
  projectId: 'prj_new',
  path: '/home/u/new-thing',
  isDefault: true,
  createdAt: '',
}

describe('quickCreateProjectPanel', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('submits project then folder and emits created', async () => {
    fetchMock
      .mockResolvedValueOnce({ ok: true, status: 201, json: async () => sampleProject })
      .mockResolvedValueOnce({ ok: true, status: 201, json: async () => sampleFolder })

    const wrapper = mount(QuickCreateProjectPanel, {
      props: { spawners: [] },
    })
    await wrapper.find('input[name="name"]').setValue('New Thing')
    await wrapper.find('input[name="path"]').setValue('/home/u/new-thing')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/projects', expect.objectContaining({ method: 'POST' }))
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      '/api/projects/prj_new/folders',
      expect.objectContaining({ method: 'POST' }),
    )
    const emitted = wrapper.emitted('created')
    expect(emitted).toBeTruthy()
    expect(emitted![0][0]).toMatchObject({ id: 'prj_new' })
  })

  it('rolls back project create when folder create fails', async () => {
    fetchMock
      .mockResolvedValueOnce({ ok: true, status: 201, json: async () => sampleProject })
      .mockResolvedValueOnce({ ok: false, status: 500, json: async () => ({ error: 'disk full' }) })
      .mockResolvedValueOnce({ ok: true, status: 204, json: async () => ({}) })

    const wrapper = mount(QuickCreateProjectPanel, {
      props: { spawners: [] },
    })
    await wrapper.find('input[name="name"]').setValue('New Thing')
    await wrapper.find('input[name="path"]').setValue('/home/u/new-thing')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledTimes(3)
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      '/api/projects/prj_new',
      expect.objectContaining({ method: 'DELETE' }),
    )
    expect(wrapper.emitted('created')).toBeFalsy()
    expect(wrapper.text()).toContain('disk full')
  })

  it('surfaces slug conflict without firing folder request', async () => {
    fetchMock.mockResolvedValueOnce({
      ok: false,
      status: 409,
      json: async () => ({ error: 'slug already exists' }),
    })

    const wrapper = mount(QuickCreateProjectPanel, {
      props: { spawners: [] },
    })
    await wrapper.find('input[name="name"]').setValue('New Thing')
    await wrapper.find('input[name="path"]').setValue('/home/u/new-thing')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('slug already exists')
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm vitest run src/components/QuickCreateProjectPanel.test.ts`
Expected: FAIL — `Cannot find module './QuickCreateProjectPanel.vue'`.

- [ ] **Step 3: Implement `QuickCreateProjectPanel.vue`**

Create `src/components/QuickCreateProjectPanel.vue`:

```vue
<script setup lang="ts">
import type { Project, ProjectFolder, Spawner } from '../types'
import { computed, ref, watch } from 'vue'
import { slugify } from '../utils/validation'
import AppButton from './ui/AppButton.vue'
import AppInput from './ui/AppInput.vue'

const props = defineProps<{ spawners: Spawner[] }>()
const emit = defineEmits<{ created: [project: Project]; cancel: [] }>()

const name = ref('')
const path = ref('')
const slug = ref('')
const slugDirty = ref(false)
const description = ref('')
const color = ref('')
const defaultSpawnerId = ref<string>('')
const isSubmitting = ref(false)
const errorMsg = ref('')

const defaultSpawnerSlug = 'claude-default'
const defaultClaudeSpawner = computed(() =>
  props.spawners.find(s => s.slug === defaultSpawnerSlug),
)

watch(name, (v) => {
  if (!slugDirty.value)
    slug.value = slugify(v)
})
watch(defaultClaudeSpawner, (v) => {
  if (v && !defaultSpawnerId.value)
    defaultSpawnerId.value = v.id
}, { immediate: true })

function onSlugInput(e: Event): void {
  slug.value = (e.target as HTMLInputElement).value
  slugDirty.value = true
}

async function submit(): Promise<void> {
  if (isSubmitting.value)
    return
  if (!name.value.trim() || !path.value.trim()) {
    errorMsg.value = 'Name and Path are required.'
    return
  }
  isSubmitting.value = true
  errorMsg.value = ''

  const projectBody: Record<string, unknown> = {
    name: name.value.trim(),
    slug: slug.value.trim() || slugify(name.value),
  }
  if (description.value.trim())
    projectBody.description = description.value.trim()
  if (color.value.trim())
    projectBody.color = color.value.trim()
  if (defaultSpawnerId.value)
    projectBody.defaultSpawnerId = defaultSpawnerId.value

  let project: Project
  try {
    const res = await fetch('/api/projects', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(projectBody),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
      throw new Error((err as { error?: string }).error || `Failed (${res.status})`)
    }
    project = await res.json() as Project
  }
  catch (e) {
    errorMsg.value = (e as Error).message
    isSubmitting.value = false
    return
  }

  try {
    const res = await fetch(`/api/projects/${project.id}/folders`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: path.value.trim(), isDefault: true }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
      throw new Error((err as { error?: string }).error || `Folder create failed (${res.status})`)
    }
    const folder = await res.json() as ProjectFolder
    emit('created', { ...project, folders: [folder] })
  }
  catch (e) {
    errorMsg.value = (e as Error).message
    // Rollback: delete the orphan project so the user can retry cleanly.
    await fetch(`/api/projects/${project.id}`, { method: 'DELETE' }).catch(() => {})
  }
  finally {
    isSubmitting.value = false
  }
}
</script>

<template>
  <div class="bg-app/60 border border-line rounded p-3 mb-4">
    <h3 class="text-xs font-semibold uppercase tracking-wider text-fg-mute mb-2">
      Create new project
    </h3>
    <form @submit.prevent="submit">
      <div class="mb-2">
        <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1" for="qcp-name">Name *</label>
        <AppInput id="qcp-name" v-model="name" name="name" required placeholder="My Project" />
      </div>
      <div class="mb-2">
        <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1" for="qcp-path">Path *</label>
        <AppInput id="qcp-path" v-model="path" name="path" required placeholder="/home/me/projects/my-project" />
      </div>
      <div class="mb-2">
        <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1" for="qcp-slug">Slug</label>
        <input
          id="qcp-slug"
          :value="slug"
          name="slug"
          class="w-full bg-app border border-line rounded text-fg text-[13px] px-2.5 py-2 leading-snug focus:outline-none focus:border-green-500"
          placeholder="auto from name"
          @input="onSlugInput"
        >
      </div>
      <div class="mb-2">
        <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1" for="qcp-spawner">Default Spawner</label>
        <select id="qcp-spawner" v-model="defaultSpawnerId" name="defaultSpawnerId" class="w-full bg-app border border-line rounded text-fg text-[13px] px-2.5 py-2">
          <option value="">
            (none)
          </option>
          <option v-for="s in spawners" :key="s.id" :value="s.id">
            {{ s.name }}
          </option>
        </select>
      </div>
      <div class="mb-2 flex gap-2">
        <div class="flex-1">
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1" for="qcp-color">Color</label>
          <input
            id="qcp-color"
            v-model="color"
            name="color"
            type="color"
            class="w-full h-9 bg-app border border-line rounded"
          >
        </div>
        <div class="flex-[3]">
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1" for="qcp-desc">Description</label>
          <AppInput id="qcp-desc" v-model="description" name="description" placeholder="(optional)" />
        </div>
      </div>
      <p v-if="errorMsg" class="text-xs text-red-600 dark:text-red-400 mb-2 leading-snug">
        {{ errorMsg }}
      </p>
      <div class="flex justify-end gap-2">
        <AppButton type="button" variant="secondary" @click="emit('cancel')">
          Cancel
        </AppButton>
        <AppButton type="submit" variant="primary" :disabled="isSubmitting || !name.trim() || !path.trim()">
          {{ isSubmitting ? 'Creating…' : 'Create' }}
        </AppButton>
      </div>
    </form>
  </div>
</template>
```

- [ ] **Step 4: Run test to verify pass**

Run: `pnpm vitest run src/components/QuickCreateProjectPanel.test.ts`
Expected: PASS for all 3 cases.

- [ ] **Step 5: Commit**

```bash
git add src/components/QuickCreateProjectPanel.vue \
        src/components/QuickCreateProjectPanel.test.ts
git commit -m "feat(spawn-dialog): add QuickCreateProjectPanel for inline project creation"
```

---

## Task 6: Frontend — Wire `SpawnDialog.vue` with project + folder + quick-create

**Files:**
- Modify: `src/components/SpawnDialog.vue`
- Create: `src/components/SpawnDialog.test.ts`

- [ ] **Step 1: Write failing component test**

Create `src/components/SpawnDialog.test.ts`:

```typescript
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import SpawnDialog from './SpawnDialog.vue'

const sampleProject = {
  id: 'prj_a',
  slug: 'alpha',
  name: 'Alpha',
  defaultSpawnerId: 'spwn_a',
  folders: [{ id: 'fld_a', projectId: 'prj_a', path: '/home/u/alpha', isDefault: true, createdAt: '' }],
  createdAt: '',
  updatedAt: '',
}

const sampleSpawner = {
  id: 'spwn_a',
  name: 'Claude (Opus)',
  slug: 'claude-opus',
  command: 'claude',
  args: [],
  env: {},
  adapterType: 'claude' as const,
  adapterConfig: {},
  modelOverride: 'claude-opus-4-7',
  builtIn: false,
  createdAt: '',
  updatedAt: '',
}

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn((url: string) => {
    if (url === '/api/projects')
      return Promise.resolve({ ok: true, json: async () => [sampleProject] })
    if (url === '/api/spawners')
      return Promise.resolve({ ok: true, json: async () => [sampleSpawner] })
    if (url === '/api/agents/spawn') {
      return Promise.resolve({
        ok: true,
        json: async () => ({ ok: true, pid: 12345 }),
      })
    }
    if (url.startsWith('/api/agents/spawn/12345/status'))
      return Promise.resolve({ ok: true, json: async () => ({ pid: 12345, status: 'running' }) })
    return Promise.resolve({ ok: true, json: async () => [] })
  }))
})
afterEach(() => vi.unstubAllGlobals())

describe('spawnDialog', () => {
  it('hydrates cwd and model when a project is selected', async () => {
    const wrapper = mount(SpawnDialog, { props: { open: true } })
    await flushPromises()

    await wrapper.find('#spawn-project').setValue('prj_a')
    await flushPromises()

    const cwdInput = wrapper.find('#spawn-cwd').element as HTMLInputElement
    expect(cwdInput.value).toBe('/home/u/alpha')

    const modelSelect = wrapper.find('#spawn-model').element as HTMLSelectElement
    expect(modelSelect.value).toBe('claude-opus-4-7')
  })

  it('sends spawnerId and projectId in the spawn payload', async () => {
    const wrapper = mount(SpawnDialog, { props: { open: true } })
    await flushPromises()
    await wrapper.find('#spawn-project').setValue('prj_a')
    await flushPromises()
    await wrapper.find('#spawn-prompt').setValue('do a thing')

    await wrapper.find('button[data-testid="spawn-btn"]').trigger('click')
    await flushPromises()

    const calls = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls
    const spawnCall = calls.find(c => c[0] === '/api/agents/spawn')
    expect(spawnCall).toBeTruthy()
    const body = JSON.parse(spawnCall![1].body as string)
    expect(body).toMatchObject({
      prompt: 'do a thing',
      cwd: '/home/u/alpha',
      model: 'claude-opus-4-7',
      spawnerId: 'spwn_a',
      projectId: 'prj_a',
    })
  })
})
```

> **Note:** the existing `Spawn Agent` button in `SpawnDialog.vue` needs a `data-testid="spawn-btn"` attribute added in the implementation step.

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm vitest run src/components/SpawnDialog.test.ts`
Expected: FAIL — project select doesn't exist (`'#spawn-project'` returns empty).

- [ ] **Step 3: Modify `SpawnDialog.vue`**

Replace the entire `<script setup lang="ts">` block of `src/components/SpawnDialog.vue` with:

```vue
<script setup lang="ts">
import type { Project } from '../types'
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { fetchProjectFolders } from '../composables/useProjectFolders'
import { useProjects } from '../composables/useProjects'
import { useSpawnDialog } from '../composables/useSpawnDialog'
import { useSpawners } from '../composables/useSpawners'
import { AVAILABLE_MODELS } from '../utils/models'
import AppButton from './ui/AppButton.vue'
import AppInput from './ui/AppInput.vue'
import AppModal from './ui/AppModal.vue'
import QuickCreateProjectPanel from './QuickCreateProjectPanel.vue'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: [] }>()

const { projects } = useProjects()
const { spawners } = useSpawners()

const dlg = useSpawnDialog({
  fetchFolders: fetchProjectFolders,
  lookupSpawner: id => spawners.value.find(s => s.id === id),
})

const projectChoice = ref<string>('')
const showQuickCreate = ref(false)
const prompt = ref('')
const systemPrompt = ref('')
const enableChannel = ref(true)
const skipPermissions = ref(false)
const skipPermissionsConfirmed = ref(false)
const isSpawning = ref(false)
const errorMsg = ref('')
const spawnStatusMsg = ref('')

let errorTimer: ReturnType<typeof setTimeout> | null = null
let statusPollTimer: ReturnType<typeof setTimeout> | null = null
let autoCloseTimer: ReturnType<typeof setTimeout> | null = null

const folderPickerVisible = computed(() => dlg.folders.value.length > 1)

function stopStatusPoll() {
  if (statusPollTimer) {
    clearTimeout(statusPollTimer)
    statusPollTimer = null
  }
}

function resetForm() {
  prompt.value = ''
  systemPrompt.value = ''
  enableChannel.value = true
  skipPermissions.value = false
  skipPermissionsConfirmed.value = false
  isSpawning.value = false
  errorMsg.value = ''
  spawnStatusMsg.value = ''
  projectChoice.value = ''
  showQuickCreate.value = false
  dlg.clearProject()
  stopStatusPoll()
  if (errorTimer) {
    clearTimeout(errorTimer)
    errorTimer = null
  }
  if (autoCloseTimer) {
    clearTimeout(autoCloseTimer)
    autoCloseTimer = null
  }
}

watch(projectChoice, async (v) => {
  if (v === '__create__') {
    showQuickCreate.value = true
    return
  }
  showQuickCreate.value = false
  if (!v) {
    dlg.clearProject()
    return
  }
  const proj = projects.value.find(p => p.id === v)
  if (proj)
    await dlg.selectProject(proj)
})

function onProjectCreated(p: Project) {
  showQuickCreate.value = false
  projectChoice.value = p.id
  // selecting via watch will hydrate cwd/model.
}

function onQuickCreateCancel() {
  showQuickCreate.value = false
  projectChoice.value = ''
}

async function pollSpawnStatus(pid: number, attempts = 0) {
  if (attempts > 15) {
    stopStatusPoll()
    return
  }
  try {
    const res = await fetch(`/api/agents/spawn/${pid}/status`)
    if (!res.ok)
      return
    const data = await res.json()
    if (data.status === 'running') {
      spawnStatusMsg.value = `Agent PID ${pid} running...`
      statusPollTimer = setTimeout(pollSpawnStatus, 2000, pid, attempts + 1)
    }
    else if (data.status === 'exited' && data.exitCode !== 0) {
      const stderr = data.stderr?.trim()
      errorMsg.value = `Agent exited with code ${data.exitCode}${stderr ? `: ${stderr.slice(-300)}` : ''}`
      spawnStatusMsg.value = ''
      isSpawning.value = false
    }
    else if (data.status === 'error') {
      errorMsg.value = data.stderr?.trim() || 'Spawn error'
      spawnStatusMsg.value = ''
      isSpawning.value = false
    }
    else {
      spawnStatusMsg.value = ''
      stopStatusPoll()
    }
  }
  catch {
    statusPollTimer = setTimeout(pollSpawnStatus, 2000, pid, attempts + 1)
  }
}

async function handleSpawn() {
  if (isSpawning.value || !prompt.value.trim() || !dlg.cwd.value.trim())
    return
  if (skipPermissions.value && !skipPermissionsConfirmed.value) {
    skipPermissionsConfirmed.value = true
    return
  }

  isSpawning.value = true
  errorMsg.value = ''
  spawnStatusMsg.value = ''

  const body: Record<string, unknown> = {
    prompt: prompt.value.trim(),
    cwd: dlg.cwd.value.trim(),
    enableChannel: enableChannel.value,
    skipPermissions: skipPermissions.value,
  }
  if (dlg.model.value)
    body.model = dlg.model.value
  if (systemPrompt.value.trim())
    body.systemPrompt = systemPrompt.value.trim()
  if (dlg.spawnerId.value)
    body.spawnerId = dlg.spawnerId.value
  if (dlg.project.value?.id)
    body.projectId = dlg.project.value.id

  try {
    const res = await fetch('/api/agents/spawn', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
    if (!res.ok) {
      const data = await res.json().catch(() => null)
      throw new Error(data?.error || `Server responded with ${res.status}`)
    }
    const data = await res.json()
    const pid = data.pid as number
    spawnStatusMsg.value = `Agent PID ${pid} spawned, verifying...`
    pollSpawnStatus(pid)
    autoCloseTimer = setTimeout(() => {
      if (isSpawning.value && !errorMsg.value) {
        resetForm()
        emit('close')
      }
    }, 3000)
  }
  catch (err: unknown) {
    errorMsg.value = err instanceof Error ? err.message : 'Failed to spawn agent'
    isSpawning.value = false
  }
}

watch(() => props.open, (isOpen) => {
  if (isOpen) {
    errorMsg.value = ''
    spawnStatusMsg.value = ''
    if (errorTimer) {
      clearTimeout(errorTimer)
      errorTimer = null
    }
  }
})

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && props.open)
    emit('close')
}
onMounted(() => window.addEventListener('keydown', onKeydown))
onUnmounted(() => {
  window.removeEventListener('keydown', onKeydown)
  stopStatusPoll()
  if (autoCloseTimer) {
    clearTimeout(autoCloseTimer)
    autoCloseTimer = null
  }
})
</script>
```

Then replace the `<template>` block of the same file with:

```vue
<template>
  <AppModal :open="open" @close="emit('close')">
    <div class="bg-card rounded-xl border border-line shadow-[0_8px_40px_rgba(0,0,0,0.5)] w-full max-w-xl">
      <header class="flex justify-between items-center px-5 py-4 border-b border-line">
        <h2 class="text-lg font-semibold text-fg">
          New Agent
        </h2>
        <button type="button" class="bg-transparent border-none text-fg-mute text-2xl cursor-pointer px-1 leading-none hover:text-fg" @click="emit('close')">
          &times;
        </button>
      </header>

      <form class="p-5" @submit.prevent>
        <div class="mb-4">
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1.5" for="spawn-prompt">Prompt</label>
          <AppInput
            id="spawn-prompt"
            v-model="prompt"
            type="textarea"
            :rows="4"
            required
            placeholder="What should the agent do?"
          />
        </div>

        <div class="mb-4">
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1.5" for="spawn-project">Project</label>
          <select id="spawn-project" v-model="projectChoice" class="w-full bg-app border border-line rounded text-fg text-[13px] px-2.5 py-2 leading-snug focus:outline-none focus:border-green-500">
            <option value="">
              — None (manual) —
            </option>
            <option v-for="p in projects" :key="p.id" :value="p.id">
              {{ p.name }}
            </option>
            <option value="__create__">
              + Create new project…
            </option>
          </select>
        </div>

        <QuickCreateProjectPanel
          v-if="showQuickCreate"
          :spawners="spawners"
          @created="onProjectCreated"
          @cancel="onQuickCreateCancel"
        />

        <div v-if="folderPickerVisible" class="mb-4">
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1.5" for="spawn-folder">Folder</label>
          <select
            id="spawn-folder"
            :value="dlg.selectedFolderId.value ?? ''"
            class="w-full bg-app border border-line rounded text-fg text-[13px] px-2.5 py-2"
            @change="dlg.selectFolder(($event.target as HTMLSelectElement).value)"
          >
            <option v-for="f in dlg.folders.value" :key="f.id" :value="f.id">
              {{ f.label || f.path }}{{ f.isDefault ? ' (default)' : '' }}
            </option>
          </select>
        </div>

        <div class="mb-4">
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1.5" for="spawn-cwd">Working Directory</label>
          <AppInput
            id="spawn-cwd"
            v-model="dlg.cwd.value"
            required
            placeholder="/path/to/project"
          />
        </div>

        <div class="mb-4">
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1.5" for="spawn-model">Model</label>
          <select id="spawn-model" v-model="dlg.model.value" class="w-full bg-app border border-line rounded text-fg text-[13px] px-2.5 py-2 leading-snug focus:outline-none focus:border-green-500">
            <option value="">
              Auto
            </option>
            <option v-for="m in AVAILABLE_MODELS" :key="m" :value="m">
              {{ m }}
            </option>
          </select>
        </div>

        <div class="mb-4">
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1.5" for="spawn-system">System Prompt</label>
          <AppInput
            id="spawn-system"
            v-model="systemPrompt"
            type="textarea"
            :rows="2"
            placeholder="Custom system instructions (optional)"
          />
        </div>

        <div class="flex items-center gap-2 mb-4">
          <input
            id="spawn-channel"
            v-model="enableChannel"
            type="checkbox"
          >
          <label for="spawn-channel">Enable dashboard control channel</label>
        </div>

        <div class="flex items-center gap-2 mb-4">
          <input
            id="spawn-yolo"
            v-model="skipPermissions"
            type="checkbox"
            @change="skipPermissionsConfirmed = false"
          >
          <label for="spawn-yolo">Skip permission prompts <span class="text-[10px] text-fg-mute font-mono">(--dangerously-skip-permissions)</span></label>
        </div>

        <div v-if="skipPermissions" class="bg-yellow-50/50 dark:bg-yellow-950/20 border border-yellow-300/60 dark:border-yellow-700/40 rounded p-2 px-3 text-xs leading-relaxed text-yellow-600 dark:text-yellow-400 mb-3">
          The agent will execute all tool calls without asking for confirmation. This includes file writes, deletions, git operations, and shell commands. Only use this in isolated environments or with trusted prompts.
        </div>

        <div v-if="skipPermissionsConfirmed" class="text-xs text-red-600 dark:text-red-400 font-semibold mb-2">
          Click "Spawn Agent" again to confirm.
        </div>

        <p v-if="spawnStatusMsg" class="text-xs text-green-600 dark:text-green-400 mt-1 leading-snug">
          {{ spawnStatusMsg }}
        </p>
        <p v-if="errorMsg" class="text-xs text-red-600 dark:text-red-400 mt-1 leading-snug whitespace-pre-wrap break-words max-h-[120px] overflow-y-auto">
          {{ errorMsg }}
        </p>
      </form>

      <footer class="flex justify-end gap-2 px-5 py-3 border-t border-line">
        <AppButton variant="secondary" @click="emit('close')">
          Cancel
        </AppButton>
        <AppButton
          data-testid="spawn-btn"
          variant="primary"
          :disabled="isSpawning || !prompt.trim() || !dlg.cwd.value.trim()"
          @click="handleSpawn"
        >
          {{ isSpawning ? 'Spawning...' : 'Spawn Agent' }}
        </AppButton>
      </footer>
    </div>
  </AppModal>
</template>
```

- [ ] **Step 4: Run tests to verify pass**

Run: `pnpm vitest run src/components/SpawnDialog.test.ts`
Expected: PASS for both cases.

- [ ] **Step 5: Run full Vitest suite + typecheck**

Run: `pnpm test && pnpm typecheck`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add src/components/SpawnDialog.vue \
        src/components/SpawnDialog.test.ts
git commit -m "feat(spawn-dialog): wire project picker, folder picker, and quick-create"
```

---

## Task 7: E2E happy-path test

**Files:**
- Create: `e2e/spawn-with-project.spec.ts`

- [ ] **Step 1: Write Playwright test**

Create `e2e/spawn-with-project.spec.ts`:

```typescript
import { expect, test } from '@playwright/test'

test('spawn dialog shows project picker and hydrates cwd from default folder', async ({ page, request }) => {
  // Pre-seed: create a project + default folder via API.
  const slug = `e2e-${Date.now()}`
  const projectRes = await request.post('/api/projects', {
    data: { name: `E2E ${slug}`, slug },
  })
  expect(projectRes.ok()).toBe(true)
  const project = await projectRes.json() as { id: string }
  const folderRes = await request.post(`/api/projects/${project.id}/folders`, {
    data: { path: '/tmp', isDefault: true },
  })
  expect(folderRes.ok()).toBe(true)

  // Open the dashboard and trigger the New Agent modal.
  await page.goto('/')
  await page.getByRole('button', { name: /new agent/i }).click()
  await expect(page.locator('#spawn-project')).toBeVisible()

  // Pick the project we just created and assert cwd auto-fills.
  await page.locator('#spawn-project').selectOption(project.id)
  await expect(page.locator('#spawn-cwd')).toHaveValue('/tmp')

  // Cancel — don't actually spawn a process in the e2e environment.
  await page.getByRole('button', { name: /^cancel$/i }).click()

  // Cleanup.
  await request.delete(`/api/projects/${project.id}`)
})
```

- [ ] **Step 2: Run the e2e test**

Run: `pnpm test:e2e e2e/spawn-with-project.spec.ts`
Expected: PASS. (Playwright will start `pnpm dev` automatically on port 13120 per `playwright.config.ts`.)

> **Note:** if the dev environment requires authentication for `/api/projects`, the test will fail with `401`. In that case, the project's existing e2e auth fixture (`e2e/dashboard.spec.ts` likely has the pattern) should be applied. If no fixture exists, add `process.env.DASHBOARD_AUTH_BYPASS=1` to the playwright `webServer.env` block in `playwright.config.ts` for the duration of this test. Document this in the PR description if applied.

- [ ] **Step 3: Commit**

```bash
git add e2e/spawn-with-project.spec.ts
git commit -m "test(e2e): cover spawn dialog project picker hydration"
```

---

## Task 8: Final integration verification

**Files:** none modified

- [ ] **Step 1: Run full Go test suite + lint**

Run: `cd server && task test && task lint`
Expected: PASS (no regressions in pipeline / mcp / api packages).

- [ ] **Step 2: Run full Vitest suite + typecheck**

Run: `pnpm test && pnpm typecheck`
Expected: PASS.

- [ ] **Step 3: Manual smoke test in dev**

Run: `task dev` and open `http://localhost:13120`.

Verify:
1. New Agent button opens modal with new Project select.
2. `— None (manual) —` keeps the old behavior (cwd stays editable, model stays Auto).
3. Selecting an existing project hydrates cwd. Multi-folder project shows the Folder select.
4. `+ Create new project…` opens the inline panel; submitting with valid Name + Path creates the project and selects it.
5. Submitting the modal POSTs `/api/agents/spawn` with `spawnerId` + `projectId` — verify in browser devtools network tab.
6. Network tab: `200` from `/api/agents/spawn`; PID returned.
7. Modal closes after ~3s.

- [ ] **Step 4: Inspect `/api/agents/spawn/{pid}/status` response**

In a separate terminal:

```bash
curl http://localhost:13120/api/agents/spawn/<PID>/status
```

Expected: JSON includes `spawnerId` field (omitted when no spawner was used).

- [ ] **Step 5: No additional commit unless the smoke test surfaced fixes**

If any fixes were applied during smoke testing, commit them with a focused message (`fix(spawn-dialog): …`).

---

## Self-Review Summary

**Spec coverage:**
- "Project select with None / projects / + Create" → Task 6 (`<select id="spawn-project">`).
- "Folder select when >1 folders" → Task 6 (`folderPickerVisible`).
- "Hydrate cwd from default folder" → Task 4 (`selectProject`).
- "Hydrate model from spawner.modelOverride" → Task 4 (`selectProject`) + Task 3 (BE override) — both sides covered.
- "Inline quick-create with rollback" → Task 5 (`QuickCreateProjectPanel.test.ts` rollback case).
- "Spawn body adds spawnerId + projectId" → Task 6 + Task 1 + Task 2 + Task 3.
- "Reject ollama/openai adapters" → Task 2.
- "Re-validate spawner command at spawn time" → Task 3.
- "Env merge (dashboard wins, secrets stripped)" → Task 3 Steps 8-9.
- "SpawnerID in SpawnStatus" → Task 3 Step 11.
- "E2E coverage" → Task 7.

No spec gaps.

**Type consistency check:**
- Composable methods used in tests and SFC: `selectProject`, `selectFolder`, `clearProject`. Match across Task 4 (impl), Task 6 (consumer). ✓
- `useSpawnDialog` returns refs: `cwd`, `model`, `spawnerId`, `folders`, `selectedFolderId`, `project`. Consumed identically in Task 6. ✓
- Spawn body keys: `prompt`, `cwd`, `model`, `systemPrompt`, `enableChannel`, `skipPermissions`, `spawnerId`, `projectId`. Match between Task 6 (FE) and Task 1-3 (BE). ✓
- Go: `spawnerRow *ent.Spawner` referenced consistently across Tasks 2-3. ✓

**Placeholder scan:** no TBDs, no "implement appropriate handling", every code block is concrete.
