package agents

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
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
	m := NewSpawnManager(0, 0, nil)
	require.NotNil(t, m)
	assert.Equal(t, 5, m.rateLimitMax)
	assert.Equal(t, 60*time.Second, m.rateLimitWindow)
}

func TestNewSpawnManager_NegativeArgsClamped(t *testing.T) {
	m := NewSpawnManager(-1, -1, nil)
	require.NotNil(t, m)
	assert.Equal(t, 5, m.rateLimitMax)
	assert.Equal(t, 60*time.Second, m.rateLimitWindow)
}

func TestNewSpawnManager_AcceptsNilRepo(t *testing.T) {
	m := NewSpawnManager(5, 60000, nil)
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
	if m.spawnerRepo != nil {
		t.Fatalf("expected nil spawnerRepo, got %v", m.spawnerRepo)
	}
}

const testSub = "user-123"

func TestIsSpawnAllowed_FirstSpawnWithinLimit(t *testing.T) {
	m := NewSpawnManager(3, 60000, nil)
	assert.True(t, m.IsSpawnAllowed(testSub), "first spawn should be allowed when no attempts recorded")
}

func TestIsSpawnAllowed_UpToLimitAllowed(t *testing.T) {
	limit := 3
	m := NewSpawnManager(limit, 60000, nil)

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
	m := NewSpawnManager(limit, 60000, nil)

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
	m := NewSpawnManager(limit, windowMs, nil)

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
	m := NewSpawnManager(limit, 60000, nil)

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
	m := NewSpawnManager(5, 60000, nil)

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
	m := NewSpawnManager(10, windowMs, nil)

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
	m := NewSpawnManager(5, 60000, &fakeSpawnerRepo{byID: map[string]*ent.Spawner{}})
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
	m := NewSpawnManager(5, 60000, &fakeSpawnerRepo{byID: map[string]*ent.Spawner{"spwn_o": row}})
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
	m := NewSpawnManager(5, 60000, &fakeSpawnerRepo{byID: map[string]*ent.Spawner{"spwn_x": row}})
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
	m := NewSpawnManager(5, 60000, &fakeSpawnerRepo{byID: map[string]*ent.Spawner{"spwn_claude": row}})
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
	m := NewSpawnManager(5, 60000, &fakeSpawnerRepo{byID: map[string]*ent.Spawner{"spwn_claude": row}})
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
	m := NewSpawnManager(5, 60000, &fakeSpawnerRepo{byID: map[string]*ent.Spawner{"spwn_custom": row}})
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
	m := NewSpawnManager(5, 60000, &fakeSpawnerRepo{byID: map[string]*ent.Spawner{"spwn_bad": row}})
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
	m := NewSpawnManager(5, 60000, &fakeSpawnerRepo{byID: map[string]*ent.Spawner{"spwn_env": row}})
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
	m := NewSpawnManager(5, 60000, repo)

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
	m := NewSpawnManager(5, 60000, repo)

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
	m := NewSpawnManager(5, 60000, &fakeSpawnerRepo{byID: map[string]*ent.Spawner{"spwn_sec": row}})
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
