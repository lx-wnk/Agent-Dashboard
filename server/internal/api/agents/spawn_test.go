package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/services"
)

// containsConsecutive returns true if args contains a followed immediately by b.
func containsConsecutive(args []string, a, b string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == a && args[i+1] == b {
			return true
		}
	}
	return false
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

// captureExec swaps execStart so the manager-built *exec.Cmd is captured
// (with its original Args/Path/Env intact) and then redirected to launch a
// benign 'true' process. The downstream goroutines see a valid cmd.Process,
// while assertions can inspect the original Args/Path/Env.
func captureExec(t *testing.T) **exec.Cmd {
	t.Helper()
	truePath, err := exec.LookPath("true")
	if err != nil {
		t.Fatalf("could not locate 'true' binary: %v", err)
	}
	var captured *exec.Cmd
	orig := execStart
	execStart = func(cmd *exec.Cmd) error {
		// Snapshot the originally-built command for assertions.
		snapshot := *cmd
		captured = &snapshot
		// Redirect the actual exec to /usr/bin/true (or wherever 'true' lives)
		// without disturbing Args/Env that consumers want to inspect.
		cmd.Path = truePath
		cmd.Args = []string{truePath}
		// exec.Command stashes a LookPath error in cmd.Err when the bare
		// binary name isn't on $PATH (e.g. CI runners without `claude`).
		// cmd.Start short-circuits on cmd.Err before trying the rewritten
		// Path, so we must clear it after rewriting.
		cmd.Err = nil
		return cmd.Start()
	}
	t.Cleanup(func() { execStart = orig })
	return &captured
}

func strPtr(s string) *string { return &s }

// fakeSpawnerRepo satisfies repo.SpawnerRepo for spawn tests.
type fakeSpawnerRepo struct {
	byID map[string]*ent.Spawner
}

func (f *fakeSpawnerRepo) Create(_ context.Context, _, _, _ string, _ []string, _ map[string]string, _, _ *string, _ string, _ map[string]string, _ bool) (*ent.Spawner, error) {
	return nil, nil
}

func (f *fakeSpawnerRepo) GetByID(_ context.Context, id string) (*ent.Spawner, error) {
	if s, ok := f.byID[id]; ok {
		return s, nil
	}
	return nil, &ent.NotFoundError{}
}

func (f *fakeSpawnerRepo) GetBySlug(_ context.Context, _ string) (*ent.Spawner, error) {
	return nil, &ent.NotFoundError{}
}

func (f *fakeSpawnerRepo) List(_ context.Context) ([]*ent.Spawner, error) {
	return nil, nil
}

func (f *fakeSpawnerRepo) Update(_ context.Context, _ string, _, _, _ *string, _ []string, _ map[string]string, _, _ *string, _ *string, _ map[string]string, _, _ bool) (*ent.Spawner, error) {
	return nil, nil
}

func (f *fakeSpawnerRepo) Delete(_ context.Context, _ string) error {
	return nil
}

func TestNewSpawnManager_DefaultsWhenInvalidArgs(t *testing.T) {
	// maxSpawns <= 0 and windowMs <= 0 should be clamped to safe defaults.
	m := NewSpawnManager(0, 0, nil, nil)
	require.NotNil(t, m)
	assert.Equal(t, 5, m.rateLimitMax)
	assert.Equal(t, 60*time.Second, m.rateLimitWindow)
}

func TestNewSpawnManager_NegativeArgsClamped(t *testing.T) {
	m := NewSpawnManager(-1, -1, nil, nil)
	require.NotNil(t, m)
	assert.Equal(t, 5, m.rateLimitMax)
	assert.Equal(t, 60*time.Second, m.rateLimitWindow)
}

func TestNewSpawnManager_AcceptsNilRepo(t *testing.T) {
	m := NewSpawnManager(5, 60000, nil, nil)
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
	if m.spawnerRepo != nil {
		t.Fatalf("expected nil spawnerRepo, got %v", m.spawnerRepo)
	}
}

const testSub = "user-123"

func TestIsSpawnAllowed_FirstSpawnWithinLimit(t *testing.T) {
	m := NewSpawnManager(3, 60000, nil, nil)
	assert.True(t, m.IsSpawnAllowed(testSub), "first spawn should be allowed when no attempts recorded")
}

func TestIsSpawnAllowed_UpToLimitAllowed(t *testing.T) {
	limit := 3
	m := NewSpawnManager(limit, 60000, nil, nil)

	// Record limit-1 attempts manually so the next check is the last allowed one.
	m.mu.Lock()
	for i := 0; i < limit-1; i++ {
		m.userAttempts[testSub] = append(m.userAttempts[testSub], time.Now())
	}
	m.mu.Unlock()

	// The (limit-1) recorded + 1 check means we're at limit-1 in the window — still allowed.
	assert.True(t, m.IsSpawnAllowed(testSub), "spawn at limit-1 recorded attempts should be allowed")
}

func TestIsSpawnAllowed_OverLimitRejected(t *testing.T) {
	limit := 3
	m := NewSpawnManager(limit, 60000, nil, nil)

	// Record exactly `limit` attempts so the next is over the limit.
	m.mu.Lock()
	for i := 0; i < limit; i++ {
		m.userAttempts[testSub] = append(m.userAttempts[testSub], time.Now())
	}
	m.mu.Unlock()

	assert.False(t, m.IsSpawnAllowed(testSub), "spawn when at or over limit should be rejected")
}

func TestIsSpawnAllowed_AfterWindowExpires_AllowedAgain(t *testing.T) {
	// Use a very short window so we can expire attempts quickly.
	windowMs := 50
	limit := 2
	m := NewSpawnManager(limit, windowMs, nil, nil)

	// Fill the window.
	m.mu.Lock()
	for i := 0; i < limit; i++ {
		m.userAttempts[testSub] = append(m.userAttempts[testSub], time.Now())
	}
	m.mu.Unlock()

	assert.False(t, m.IsSpawnAllowed(testSub), "should be rate-limited before window expires")

	// Wait for the window to expire.
	time.Sleep(time.Duration(windowMs+20) * time.Millisecond)

	assert.True(t, m.IsSpawnAllowed(testSub), "should be allowed after rate window expires")
}

func TestIsSpawnAllowed_PerUser_Isolated(t *testing.T) {
	limit := 2
	m := NewSpawnManager(limit, 60000, nil, nil)

	// Fill the limit for user-A.
	m.mu.Lock()
	for i := 0; i < limit; i++ {
		m.userAttempts["user-A"] = append(m.userAttempts["user-A"], time.Now())
	}
	m.mu.Unlock()

	assert.False(t, m.IsSpawnAllowed("user-A"), "user-A should be rate-limited")
	assert.True(t, m.IsSpawnAllowed("user-B"), "user-B should not be affected by user-A's limit")
}

// TestSendMessageToChannel_RespectsContextCancellation verifies that a pre-cancelled
// context causes SendMessageToChannel to return promptly rather than blocking.
// The function must not hang even if a network call is involved.
func TestSendMessageToChannel_RespectsContextCancellation(t *testing.T) {
	m := NewSpawnManager(5, 60000, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately before the call

	// PID 99999 almost certainly doesn't exist; the function will fail while
	// trying to read the discovery file — but the important contract is that it
	// returns an error promptly (not block) when the context is already cancelled.
	err := m.SendMessageToChannel(ctx, 99999, "ping")
	require.Error(t, err, "SendMessageToChannel must return an error for an unknown PID")

	// Ensure the call did not block: if we got here at all, the test passes.
	// An additional context-awareness check: if the implementation ever reaches
	// an HTTP call, a pre-cancelled context causes the request to fail with a
	// context error. Verify no deadline-exceeded style hang occurred by running
	// the whole call with a tight deadline.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	cancel2() // cancel immediately

	err2 := m.SendMessageToChannel(ctx2, 99999, "ping")
	require.Error(t, err2, "SendMessageToChannel must return an error with a cancelled deadline context")
}

func TestPruneAttempts_RemovesOldEntries(t *testing.T) {
	windowMs := 50
	m := NewSpawnManager(10, windowMs, nil, nil)

	// Add one old attempt (pre-window) and one fresh one.
	old := time.Now().Add(-200 * time.Millisecond)
	m.mu.Lock()
	m.userAttempts[testSub] = append(m.userAttempts[testSub], old)
	m.userAttempts[testSub] = append(m.userAttempts[testSub], time.Now())
	m.mu.Unlock()

	// Wait for window to cover the "old" timestamp.
	time.Sleep(time.Duration(windowMs+10) * time.Millisecond)

	m.mu.Lock()
	m.pruneAttempts(testSub)
	count := len(m.userAttempts[testSub])
	m.mu.Unlock()

	assert.Equal(t, 0, count, "all attempts older than window should be pruned")
}

func TestSpawn_UnknownSpawnerID_Returns400(t *testing.T) {
	tmp, _ := filepath.EvalSymlinks(os.TempDir())
	t.Setenv("HOME", tmp)
	m := NewSpawnManager(5, 60000, &fakeSpawnerRepo{byID: map[string]*ent.Spawner{}}, nil)
	_, err := m.Spawn("u1", map[string]any{
		"prompt":    "do thing",
		"cwd":       tmp,
		"spawnerId": "spwn_missing",
	})
	if err == nil || !strings.Contains(err.Error(), "spawner not found") {
		t.Fatalf("expected 'spawner not found' error, got %v", err)
	}
}

func TestSpawn_OllamaAdapter_Rejected(t *testing.T) {
	tmp, _ := filepath.EvalSymlinks(os.TempDir())
	t.Setenv("HOME", tmp)
	row := &ent.Spawner{ID: "spwn_o", AdapterType: "ollama", Command: "claude"}
	m := NewSpawnManager(5, 60000, &fakeSpawnerRepo{byID: map[string]*ent.Spawner{"spwn_o": row}}, nil)
	_, err := m.Spawn("u1", map[string]any{
		"prompt":    "do thing",
		"cwd":       tmp,
		"spawnerId": "spwn_o",
	})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected adapter-not-supported error, got %v", err)
	}
}

func TestSpawn_OpenAIAdapter_Rejected(t *testing.T) {
	tmp, _ := filepath.EvalSymlinks(os.TempDir())
	t.Setenv("HOME", tmp)
	row := &ent.Spawner{ID: "spwn_x", AdapterType: "openai", Command: "claude"}
	m := NewSpawnManager(5, 60000, &fakeSpawnerRepo{byID: map[string]*ent.Spawner{"spwn_x": row}}, nil)
	_, err := m.Spawn("u1", map[string]any{
		"prompt":    "do thing",
		"cwd":       tmp,
		"spawnerId": "spwn_x",
	})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected adapter-not-supported error, got %v", err)
	}
}

func TestSpawn_ClaudeAdapter_HydratesModelFromOverride(t *testing.T) {
	tmp, _ := filepath.EvalSymlinks(os.TempDir())
	t.Setenv("HOME", tmp)
	captured := captureExec(t)

	row := &ent.Spawner{
		ID:            "spwn_claude",
		AdapterType:   "claude",
		Command:       "claude",
		ModelOverride: strPtr("claude-opus-4-7"),
	}
	m := NewSpawnManager(5, 60000, &fakeSpawnerRepo{byID: map[string]*ent.Spawner{"spwn_claude": row}}, nil)
	_, err := m.Spawn("u1", map[string]any{
		"prompt":        "do thing",
		"cwd":           tmp,
		"spawnerId":     "spwn_claude",
		"enableChannel": false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	require.NotNil(t, *captured, "expected execStart to be called")
	assert.True(t,
		containsConsecutive((*captured).Args, "--model", "claude-opus-4-7"),
		"expected --model claude-opus-4-7 in args, got %v", (*captured).Args,
	)
}

func TestSpawn_BodyModelOverridesSpawnerModelOverride(t *testing.T) {
	tmp, _ := filepath.EvalSymlinks(os.TempDir())
	t.Setenv("HOME", tmp)
	captured := captureExec(t)

	row := &ent.Spawner{
		ID:            "spwn_claude",
		AdapterType:   "claude",
		Command:       "claude",
		ModelOverride: strPtr("claude-opus-4-7"),
	}
	m := NewSpawnManager(5, 60000, &fakeSpawnerRepo{byID: map[string]*ent.Spawner{"spwn_claude": row}}, nil)
	_, err := m.Spawn("u1", map[string]any{
		"prompt":        "do thing",
		"cwd":           tmp,
		"spawnerId":     "spwn_claude",
		"model":         "claude-sonnet-4-6",
		"enableChannel": false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	require.NotNil(t, *captured)
	assert.True(t,
		containsConsecutive((*captured).Args, "--model", "claude-sonnet-4-6"),
		"expected body model to win, got %v", (*captured).Args,
	)
	for _, a := range (*captured).Args {
		assert.NotEqual(t, "claude-opus-4-7", a, "spawner override must not appear when body model is set")
	}
}

func TestSpawn_CustomAdapter_UsesSpawnerCommand(t *testing.T) {
	tmp, _ := filepath.EvalSymlinks(os.TempDir())
	t.Setenv("HOME", tmp)
	t.Setenv("DASHBOARD_SPAWNER_ALLOWED_COMMANDS", "npx")
	captured := captureExec(t)

	row := &ent.Spawner{
		ID:          "spwn_custom",
		AdapterType: "custom",
		Command:     "npx",
		Args:        []string{"--", "claude-clone"},
	}
	m := NewSpawnManager(5, 60000, &fakeSpawnerRepo{byID: map[string]*ent.Spawner{"spwn_custom": row}}, nil)
	_, err := m.Spawn("u1", map[string]any{
		"prompt":        "do thing",
		"cwd":           tmp,
		"spawnerId":     "spwn_custom",
		"enableChannel": false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	require.NotNil(t, *captured)
	path := (*captured).Path
	assert.True(t, path == "npx" || strings.HasSuffix(path, "/npx"),
		"expected captured.Path to be 'npx' or end with '/npx', got %q", path)
	assert.True(t,
		containsConsecutive((*captured).Args, "--", "claude-clone"),
		"expected spawner args '-- claude-clone' before canonical args, got %v", (*captured).Args,
	)
}

func TestSpawn_CustomAdapter_DisallowedCommandRejected(t *testing.T) {
	tmp, _ := filepath.EvalSymlinks(os.TempDir())
	t.Setenv("HOME", tmp)
	t.Setenv("DASHBOARD_SPAWNER_ALLOWED_COMMANDS", "")

	row := &ent.Spawner{
		ID:          "spwn_bad",
		AdapterType: "custom",
		Command:     "rm",
	}
	m := NewSpawnManager(5, 60000, &fakeSpawnerRepo{byID: map[string]*ent.Spawner{"spwn_bad": row}}, nil)
	_, err := m.Spawn("u1", map[string]any{
		"prompt":        "do thing",
		"cwd":           tmp,
		"spawnerId":     "spwn_bad",
		"enableChannel": false,
	})
	if err == nil || !strings.Contains(err.Error(), "not permitted") {
		t.Fatalf("expected 'not permitted' error, got %v", err)
	}
}

func TestSpawn_EnvMerge_DashboardWins(t *testing.T) {
	tmp, _ := filepath.EvalSymlinks(os.TempDir())
	t.Setenv("HOME", tmp)
	t.Setenv("DASHBOARD_MCP_TOKEN", "from-dashboard")
	t.Setenv("DASHBOARD_SPAWNER_ALLOWED_COMMANDS", "claude")
	captured := captureExec(t)

	row := &ent.Spawner{
		ID:          "spwn_env",
		AdapterType: "claude",
		Command:     "claude",
		Env: map[string]string{
			"DASHBOARD_MCP_TOKEN": "from-spawner",
			"MY_CUSTOM":           "hello",
		},
	}
	m := NewSpawnManager(5, 60000, &fakeSpawnerRepo{byID: map[string]*ent.Spawner{"spwn_env": row}}, nil)
	_, err := m.Spawn("u1", map[string]any{
		"prompt":        "do thing",
		"cwd":           tmp,
		"spawnerId":     "spwn_env",
		"enableChannel": false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	require.NotNil(t, *captured)
	assert.Equal(t, "from-dashboard", envValue((*captured).Env, "DASHBOARD_MCP_TOKEN"),
		"dashboard env var must win over spawner env")
	assert.Equal(t, "hello", envValue((*captured).Env, "MY_CUSTOM"),
		"non-conflicting spawner env var must be present")
}

func TestMergeEnv_NilSpawner_StripsSecrets(t *testing.T) {
	t.Setenv("DASHBOARD_JWT_SECRET", "x")
	t.Setenv("DASHBOARD_HOOKS_SECRET", "y")
	t.Setenv("DASHBOARD_KEEP_ME", "z")

	env := mergeEnv(nil)
	if envValue(env, "DASHBOARD_JWT_SECRET") != "" {
		t.Fatalf("JWT secret must be stripped on nil-spawner path")
	}
	if envValue(env, "DASHBOARD_HOOKS_SECRET") != "" {
		t.Fatalf("hooks secret must be stripped on nil-spawner path")
	}
	if envValue(env, "DASHBOARD_KEEP_ME") != "z" {
		t.Fatalf("non-secret DASHBOARD vars must survive, got %q", envValue(env, "DASHBOARD_KEEP_ME"))
	}
}

func TestSpawn_CustomAdapter_ReservedFlagRejected(t *testing.T) {
	t.Setenv("DASHBOARD_SPAWNER_ALLOWED_COMMANDS", "npx")
	row := &ent.Spawner{
		ID: "spwn_bad", AdapterType: "custom", Command: "npx",
		Args: []string{"--model", "anything"}, // reserved
	}
	repo := &fakeSpawnerRepo{byID: map[string]*ent.Spawner{"spwn_bad": row}}
	m := NewSpawnManager(5, 60000, repo, nil)

	tmp, _ := filepath.EvalSymlinks(os.TempDir())
	t.Setenv("HOME", tmp)
	_, err := m.Spawn("u1", map[string]any{
		"prompt":    "do thing",
		"cwd":       tmp,
		"spawnerId": "spwn_bad",
	})
	if err == nil || !strings.Contains(err.Error(), "reserved flag") {
		t.Fatalf("expected reserved-flag rejection, got %v", err)
	}
}

func TestSpawn_CustomAdapter_ChannelArgOverride(t *testing.T) {
	t.Setenv("DASHBOARD_SPAWNER_ALLOWED_COMMANDS", "npx")
	row := &ent.Spawner{
		ID: "spwn_arg", AdapterType: "custom", Command: "npx",
		AdapterConfig: map[string]string{"channel_arg": "--config"},
	}
	repo := &fakeSpawnerRepo{byID: map[string]*ent.Spawner{"spwn_arg": row}}
	m := NewSpawnManager(5, 60000, repo, nil)

	tmp, _ := filepath.EvalSymlinks(os.TempDir())
	t.Setenv("HOME", tmp)
	capturedPtr := captureExec(t)

	_, err := m.Spawn("u1", map[string]any{
		"prompt":        "do thing",
		"cwd":           tmp,
		"spawnerId":     "spwn_arg",
		"enableChannel": true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	captured := *capturedPtr
	if captured == nil {
		t.Fatal("execStart not invoked")
	}
	// The flag name from adapter_config["channel_arg"] should be present
	// immediately before the config path.
	found := false
	for i := 0; i+1 < len(captured.Args); i++ {
		if captured.Args[i] == "--config" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected --config (channel_arg override), got args %v", captured.Args)
	}
	// Default --mcp-config must NOT appear.
	for _, a := range captured.Args {
		if a == "--mcp-config" {
			t.Fatalf("--mcp-config must not appear when channel_arg overrides it, got args %v", captured.Args)
		}
	}
}

// TestSpawnHandler_CwdOutsideAllowlist_Returns403 is an integration test that
// wires a real SpawnPolicy (with a single fixed allowed root) into a SpawnHandler
// and verifies that a spawn request whose cwd falls outside the allowed root is
// rejected with HTTP 403 (8d.A).
func TestSpawnHandler_CwdOutsideAllowlist_Returns403(t *testing.T) {
	tmp, err := filepath.EvalSymlinks(os.TempDir())
	require.NoError(t, err)

	// allowedRoot is the only directory the policy will accept.
	allowedRoot := filepath.Join(tmp, "allowed-root")
	require.NoError(t, os.MkdirAll(allowedRoot, 0o755))

	// outsideDir exists on disk but is NOT under allowedRoot.
	outsideDir := filepath.Join(tmp, "outside-dir")
	require.NoError(t, os.MkdirAll(outsideDir, 0o755))

	// Point HOME somewhere that has no sensitive dirs, so the blacklist doesn't
	// interfere with this test's focus on allow-list rejection.
	homeDir := filepath.Join(tmp, "testhome")
	require.NoError(t, os.MkdirAll(homeDir, 0o755))
	t.Setenv("HOME", homeDir)

	policy := services.NewSpawnPolicy(func(_ context.Context) ([]string, error) {
		return []string{allowedRoot}, nil
	})

	manager := NewSpawnManager(5, 60000, nil, policy)
	handler := NewSpawnHandler(manager)

	body, _ := json.Marshal(map[string]any{
		"prompt":        "do something",
		"cwd":           outsideDir,
		"enableChannel": false,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/agents/spawn", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.Spawn(rr, req)

	require.Equal(t, http.StatusForbidden, rr.Code,
		"expected HTTP 403 when cwd is outside all allowed project roots, got %d — body: %s",
		rr.Code, rr.Body.String())
}

// fakeProjectFolderRepo satisfies repo.ProjectFolderRepo for spawn tests.
// Only ListByProject is meaningful; the rest panic to catch unintended calls.
type fakeProjectFolderRepo struct {
	// byProject maps projectID → folders returned by ListByProject.
	byProject map[string][]*ent.ProjectFolder
	// listErr, when non-nil, is returned by ListByProject.
	listErr error
}

func (f *fakeProjectFolderRepo) Create(_ context.Context, _, _ string, _ *string, _ bool) (*ent.ProjectFolder, error) {
	panic("fakeProjectFolderRepo.Create not implemented")
}
func (f *fakeProjectFolderRepo) GetByID(_ context.Context, _ string) (*ent.ProjectFolder, error) {
	panic("fakeProjectFolderRepo.GetByID not implemented")
}
func (f *fakeProjectFolderRepo) ListByProject(_ context.Context, projectID string) ([]*ent.ProjectFolder, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.byProject[projectID], nil
}
func (f *fakeProjectFolderRepo) ListAll(_ context.Context) ([]*ent.ProjectFolder, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var all []*ent.ProjectFolder
	for _, folders := range f.byProject {
		all = append(all, folders...)
	}
	return all, nil
}
func (f *fakeProjectFolderRepo) Update(_ context.Context, _ string, _, _ *string, _ bool, _ *bool) (*ent.ProjectFolder, error) {
	panic("fakeProjectFolderRepo.Update not implemented")
}
func (f *fakeProjectFolderRepo) Delete(_ context.Context, _ string) error {
	panic("fakeProjectFolderRepo.Delete not implemented")
}
func (f *fakeProjectFolderRepo) Suggest(_ context.Context, _ string) ([]*ent.ProjectFolder, error) {
	panic("fakeProjectFolderRepo.Suggest not implemented")
}

func newFolder(path string) *ent.ProjectFolder { return &ent.ProjectFolder{Path: path} }

// containsFlag returns true if args contains flag followed by value.
func containsFlag(args []string, flag, value string) bool {
	return containsConsecutive(args, flag, value)
}

func TestSpawn_AdditionalDirs_InjectedForMultiFolderProject(t *testing.T) {
	// Use a real TempDir so os.Stat succeeds; create separate subdirs for cwd and
	// extra folders so each exists on disk (spawn.go calls os.Stat(cwd)).
	base := t.TempDir()
	cwd := filepath.Join(base, "web")
	shared := filepath.Join(base, "shared")
	docs := filepath.Join(base, "docs")
	require.NoError(t, os.MkdirAll(cwd, 0o755))
	require.NoError(t, os.MkdirAll(shared, 0o755))
	require.NoError(t, os.MkdirAll(docs, 0o755))

	// Resolve symlinks so HOME and cwd comparisons are consistent.
	cwd, _ = filepath.EvalSymlinks(cwd)
	shared, _ = filepath.EvalSymlinks(shared)
	docs, _ = filepath.EvalSymlinks(docs)
	base, _ = filepath.EvalSymlinks(base)

	t.Setenv("HOME", base)
	capturedPtr := captureExec(t)

	folderRepo := &fakeProjectFolderRepo{
		byProject: map[string][]*ent.ProjectFolder{
			"proj-1": {newFolder(cwd), newFolder(shared), newFolder(docs)},
		},
	}

	m := NewSpawnManager(5, 60000, nil, nil)
	m.SetProjectFolderRepo(folderRepo)

	_, err := m.Spawn("u1", map[string]any{
		"prompt":        "do thing",
		"cwd":           cwd,
		"projectId":     "proj-1",
		"enableChannel": false,
	})
	require.NoError(t, err)

	captured := *capturedPtr
	require.NotNil(t, captured, "expected execStart to be called")

	assert.True(t, containsFlag(captured.Args, "--add-dir", shared),
		"expected --add-dir %s in args %v", shared, captured.Args)
	assert.True(t, containsFlag(captured.Args, "--add-dir", docs),
		"expected --add-dir %s in args %v", docs, captured.Args)

	// cwd itself must NOT appear as --add-dir
	for i := 0; i+1 < len(captured.Args); i++ {
		if captured.Args[i] == "--add-dir" && captured.Args[i+1] == cwd {
			t.Fatalf("cwd %q must not appear as --add-dir, got args %v", cwd, captured.Args)
		}
	}
}

func TestSpawn_AdditionalDirs_NotInjectedWithoutProjectId(t *testing.T) {
	base := t.TempDir()
	cwd, _ := filepath.EvalSymlinks(base)
	t.Setenv("HOME", base)
	capturedPtr := captureExec(t)

	folderRepo := &fakeProjectFolderRepo{
		byProject: map[string][]*ent.ProjectFolder{
			"proj-x": {newFolder(cwd), newFolder(filepath.Join(base, "extra"))},
		},
	}

	m := NewSpawnManager(5, 60000, nil, nil)
	m.SetProjectFolderRepo(folderRepo)

	_, err := m.Spawn("u1", map[string]any{
		"prompt": "do thing",
		"cwd":    cwd,
		// no projectId
		"enableChannel": false,
	})
	require.NoError(t, err)

	captured := *capturedPtr
	require.NotNil(t, captured)
	for _, a := range captured.Args {
		assert.NotEqual(t, "--add-dir", a,
			"--add-dir must not appear when no projectId is provided, got args %v", captured.Args)
	}
}

func TestSpawn_AdditionalDirs_NotInjectedWithNilRepo(t *testing.T) {
	base := t.TempDir()
	cwd, _ := filepath.EvalSymlinks(base)
	t.Setenv("HOME", base)
	capturedPtr := captureExec(t)

	// SpawnManager without SetProjectFolderRepo → repo is nil
	m := NewSpawnManager(5, 60000, nil, nil)

	_, err := m.Spawn("u1", map[string]any{
		"prompt":        "do thing",
		"cwd":           cwd,
		"projectId":     "proj-1",
		"enableChannel": false,
	})
	require.NoError(t, err)

	captured := *capturedPtr
	require.NotNil(t, captured)
	for _, a := range captured.Args {
		assert.NotEqual(t, "--add-dir", a,
			"--add-dir must not appear when projectFolderRepo is nil, got args %v", captured.Args)
	}
}

func TestSpawn_AdditionalDirs_RepoErrorSkipped(t *testing.T) {
	// When the repo returns an error, spawn should succeed (warn + continue).
	base := t.TempDir()
	cwd, _ := filepath.EvalSymlinks(base)
	t.Setenv("HOME", base)
	capturedPtr := captureExec(t)

	folderRepo := &fakeProjectFolderRepo{
		listErr: errors.New("db unavailable"),
	}

	m := NewSpawnManager(5, 60000, nil, nil)
	m.SetProjectFolderRepo(folderRepo)

	_, err := m.Spawn("u1", map[string]any{
		"prompt":        "do thing",
		"cwd":           cwd,
		"projectId":     "proj-1",
		"enableChannel": false,
	})
	require.NoError(t, err, "repo error must not block spawn")

	captured := *capturedPtr
	require.NotNil(t, captured)
	for _, a := range captured.Args {
		assert.NotEqual(t, "--add-dir", a,
			"--add-dir must not appear when repo errors, got args %v", captured.Args)
	}
}

// countOccurrences returns the number of times s appears in args.
func countOccurrences(args []string, s string) int {
	n := 0
	for _, a := range args {
		if a == s {
			n++
		}
	}
	return n
}

func TestSpawn_PermissionMode_ExplicitAcceptEdits(t *testing.T) {
	base := t.TempDir()
	cwd, _ := filepath.EvalSymlinks(base)
	t.Setenv("HOME", base)
	capturedPtr := captureExec(t)

	m := NewSpawnManager(5, 60000, nil, nil)
	_, err := m.Spawn("u1", map[string]any{
		"prompt":         "do thing",
		"cwd":            cwd,
		"permissionMode": "acceptEdits",
		"enableChannel":  false,
	})
	require.NoError(t, err)
	captured := *capturedPtr
	require.NotNil(t, captured)
	assert.True(t,
		containsConsecutive(captured.Args, "--permission-mode", "acceptEdits"),
		"expected --permission-mode acceptEdits in args %v", captured.Args)
}

func TestSpawn_PermissionMode_AbsentDefaultsToDefault(t *testing.T) {
	base := t.TempDir()
	cwd, _ := filepath.EvalSymlinks(base)
	t.Setenv("HOME", base)
	capturedPtr := captureExec(t)

	m := NewSpawnManager(5, 60000, nil, nil)
	_, err := m.Spawn("u1", map[string]any{
		"prompt":        "do thing",
		"cwd":           cwd,
		"enableChannel": false,
		// no permissionMode key
	})
	require.NoError(t, err)
	captured := *capturedPtr
	require.NotNil(t, captured)
	assert.True(t,
		containsConsecutive(captured.Args, "--permission-mode", "default"),
		"expected --permission-mode default when key absent, got %v", captured.Args)
}

func TestSpawn_PermissionMode_BypassPermissions(t *testing.T) {
	base := t.TempDir()
	cwd, _ := filepath.EvalSymlinks(base)
	t.Setenv("HOME", base)
	capturedPtr := captureExec(t)

	m := NewSpawnManager(5, 60000, nil, nil)
	_, err := m.Spawn("u1", map[string]any{
		"prompt":         "do thing",
		"cwd":            cwd,
		"permissionMode": "bypassPermissions",
		"enableChannel":  false,
	})
	require.NoError(t, err)
	captured := *capturedPtr
	require.NotNil(t, captured)
	assert.True(t,
		containsConsecutive(captured.Args, "--permission-mode", "bypassPermissions"),
		"expected --permission-mode bypassPermissions in args %v", captured.Args)
}

func TestSpawn_PermissionMode_AutoAndDontAsk(t *testing.T) {
	for _, mode := range []string{"auto", "dontAsk", "plan"} {
		t.Run(mode, func(t *testing.T) {
			base := t.TempDir()
			cwd, _ := filepath.EvalSymlinks(base)
			t.Setenv("HOME", base)
			capturedPtr := captureExec(t)

			m := NewSpawnManager(5, 60000, nil, nil)
			_, err := m.Spawn("u1", map[string]any{
				"prompt":         "do thing",
				"cwd":            cwd,
				"permissionMode": mode,
				"enableChannel":  false,
			})
			require.NoError(t, err, "%s must be an accepted permission mode", mode)
			captured := *capturedPtr
			require.NotNil(t, captured)
			assert.True(t,
				containsConsecutive(captured.Args, "--permission-mode", mode),
				"expected --permission-mode %s in args %v", mode, captured.Args)
		})
	}
}

func TestSpawn_PermissionMode_InvalidReturnsError(t *testing.T) {
	base := t.TempDir()
	cwd, _ := filepath.EvalSymlinks(base)
	t.Setenv("HOME", base)
	capturedPtr := captureExec(t)

	m := NewSpawnManager(5, 60000, nil, nil)
	_, err := m.Spawn("u1", map[string]any{
		"prompt":         "do thing",
		"cwd":            cwd,
		"permissionMode": "nonsense",
		"enableChannel":  false,
	})
	require.Error(t, err, "invalid permissionMode must return an error")
	assert.Contains(t, err.Error(), "invalid permissionMode")
	assert.Nil(t, *capturedPtr, "no process must be started on invalid permissionMode")
}

func TestSpawn_PermissionMode_SpawnerOwnsPermissionMode_NotDoubled(t *testing.T) {
	base := t.TempDir()
	cwd, _ := filepath.EvalSymlinks(base)
	t.Setenv("HOME", base)
	t.Setenv("DASHBOARD_SPAWNER_ALLOWED_COMMANDS", "claude")
	capturedPtr := captureExec(t)

	// Spawner declares its own --permission-mode acceptEdits; dashboard must not
	// append a second --permission-mode flag.
	row := &ent.Spawner{
		ID:          "spwn_pm",
		AdapterType: "claude",
		Command:     "claude",
		Args:        []string{"--permission-mode", "acceptEdits"},
	}
	m := NewSpawnManager(5, 60000, &fakeSpawnerRepo{byID: map[string]*ent.Spawner{"spwn_pm": row}}, nil)
	_, err := m.Spawn("u1", map[string]any{
		"prompt":        "do thing",
		"cwd":           cwd,
		"spawnerId":     "spwn_pm",
		"enableChannel": false,
		// caller also sends a permissionMode — must be overridden by spawner guard
		"permissionMode": "default",
	})
	require.NoError(t, err)
	captured := *capturedPtr
	require.NotNil(t, captured)
	assert.Equal(t, 1, countOccurrences(captured.Args, "--permission-mode"),
		"--permission-mode must appear exactly once (spawner's own), got args %v", captured.Args)
	assert.True(t,
		containsConsecutive(captured.Args, "--permission-mode", "acceptEdits"),
		"spawner's --permission-mode acceptEdits must be preserved, got args %v", captured.Args)
}

func TestSpawn_PermissionMode_SpawnerDangerouslySkip_NotDoubled(t *testing.T) {
	base := t.TempDir()
	cwd, _ := filepath.EvalSymlinks(base)
	t.Setenv("HOME", base)
	t.Setenv("DASHBOARD_SPAWNER_ALLOWED_COMMANDS", "claude")
	capturedPtr := captureExec(t)

	row := &ent.Spawner{
		ID:          "spwn_skip",
		AdapterType: "claude",
		Command:     "claude",
		Args:        []string{"--dangerously-skip-permissions"},
	}
	m := NewSpawnManager(5, 60000, &fakeSpawnerRepo{byID: map[string]*ent.Spawner{"spwn_skip": row}}, nil)
	_, err := m.Spawn("u1", map[string]any{
		"prompt":        "do thing",
		"cwd":           cwd,
		"spawnerId":     "spwn_skip",
		"enableChannel": false,
	})
	require.NoError(t, err)
	captured := *capturedPtr
	require.NotNil(t, captured)
	assert.Equal(t, 0, countOccurrences(captured.Args, "--permission-mode"),
		"--permission-mode must not be appended when spawner uses --dangerously-skip-permissions, got args %v", captured.Args)
}

func TestSpawn_EnvMerge_SecretsStripped(t *testing.T) {
	tmp, _ := filepath.EvalSymlinks(os.TempDir())
	t.Setenv("HOME", tmp)
	t.Setenv("DASHBOARD_JWT_SECRET", "x")
	t.Setenv("DASHBOARD_HOOKS_SECRET", "y")
	t.Setenv("DASHBOARD_SPAWNER_ALLOWED_COMMANDS", "claude")
	captured := captureExec(t)

	row := &ent.Spawner{
		ID:          "spwn_sec",
		AdapterType: "claude",
		Command:     "claude",
	}
	m := NewSpawnManager(5, 60000, &fakeSpawnerRepo{byID: map[string]*ent.Spawner{"spwn_sec": row}}, nil)
	_, err := m.Spawn("u1", map[string]any{
		"prompt":        "do thing",
		"cwd":           tmp,
		"spawnerId":     "spwn_sec",
		"enableChannel": false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	require.NotNil(t, *captured)
	assert.Equal(t, "", envValue((*captured).Env, "DASHBOARD_JWT_SECRET"),
		"DASHBOARD_JWT_SECRET must be stripped from child env")
	assert.Equal(t, "", envValue((*captured).Env, "DASHBOARD_HOOKS_SECRET"),
		"DASHBOARD_HOOKS_SECRET must be stripped from child env")
}
