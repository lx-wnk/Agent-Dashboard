package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/channelconfig"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/httputil"
	"github.com/lx-wnk/agent-dashboard/server/internal/services"
)

const (
	spawnStoreMaxAge  = time.Hour
	maxStderrBytes    = 4096
	systemPromptMax   = 10000
	channelMsgTimeout = 5 * time.Second
)

var (
	claudeBin = func() string {
		if p, err := exec.LookPath("claude"); err == nil {
			return p
		}
		return "claude"
	}()
	uuidRE = regexp.MustCompile(`(?i)^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
)

// execStart is the seam used by tests to intercept spawn args without
// actually launching a process. Production uses cmd.Start directly.
var execStart = func(cmd *exec.Cmd) error { return cmd.Start() }

// SpawnStatus tracks the state of a user-initiated agent spawn.
type SpawnStatus struct {
	PID       int    `json:"pid"`
	Status    string `json:"status"` // "running" | "exited" | "error"
	ExitCode  *int   `json:"exitCode"`
	Stderr    string `json:"stderr"`
	StartedAt string `json:"startedAt"`
	Prompt    string `json:"prompt"`
	Cwd       string `json:"cwd"`
	SpawnerID string `json:"spawnerId,omitempty"`
}

// SpawnManager rate-limits and tracks user-initiated Claude agent spawns.
type SpawnManager struct {
	rateLimitMax      int
	rateLimitWindow   time.Duration
	spawnerRepo       repo.SpawnerRepo
	spawnPolicy       services.SpawnPolicy
	projectFolderRepo repo.ProjectFolderRepo // may be nil

	mu           sync.Mutex
	userAttempts map[string][]time.Time // per-user sliding window keyed by JWT sub (or "__global__" in bypass mode)
	spawnStore   map[int]*SpawnStatus
}

// NewSpawnManager creates a SpawnManager with the given rate limit config.
// policy controls which working directories may be used as spawn cwd; pass nil
// to enforce only the sensitive-dir blacklist (development / bypass-auth mode).
func NewSpawnManager(maxSpawns int, windowMs int, spawnerRepo repo.SpawnerRepo, policy services.SpawnPolicy) *SpawnManager {
	if maxSpawns <= 0 {
		maxSpawns = 5
	}
	if windowMs <= 0 {
		windowMs = 60_000
	}
	if policy == nil {
		policy = services.NewSpawnPolicy(nil)
	}
	return &SpawnManager{
		rateLimitMax:    maxSpawns,
		rateLimitWindow: time.Duration(windowMs) * time.Millisecond,
		spawnerRepo:     spawnerRepo,
		spawnPolicy:     policy,
		userAttempts:    make(map[string][]time.Time),
		spawnStore:      make(map[int]*SpawnStatus),
	}
}

// SetProjectFolderRepo wires in a ProjectFolderRepo so that Spawn can inject
// --add-dir flags for multi-folder projects. Call once after NewSpawnManager
// before any spawns; safe to call with nil (disables --add-dir injection).
func (m *SpawnManager) SetProjectFolderRepo(r repo.ProjectFolderRepo) {
	m.projectFolderRepo = r
}

// IsSpawnAllowed reports whether a new spawn is allowed for the given user (sub).
// Pass "__global__" in bypass-auth mode.
func (m *SpawnManager) IsSpawnAllowed(sub string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneAttempts(sub)
	return len(m.userAttempts[sub]) < m.rateLimitMax
}

func (m *SpawnManager) recordAttempt(sub string) {
	m.pruneAttempts(sub)
	m.userAttempts[sub] = append(m.userAttempts[sub], time.Now())
}

func (m *SpawnManager) pruneAttempts(sub string) {
	cutoff := time.Now().Add(-m.rateLimitWindow)
	attempts := m.userAttempts[sub]
	i := 0
	for i < len(attempts) && attempts[i].Before(cutoff) {
		i++
	}
	if i > 0 {
		m.userAttempts[sub] = attempts[i:]
	}
}

// spawnRequest carries validated fields extracted from the raw request body.
type spawnRequest struct {
	prompt          string
	cwd             string
	resumeSessionID string
	model           string
	systemPrompt    string
	permissionMode  string
	projectID       string
	enableChannel   bool
}

// enforceSpawnPolicy validates the request body fields and applies the spawn
// policy gate. Rate-limit recordAttempt must be called before this method.
func (m *SpawnManager) enforceSpawnPolicy(body map[string]any) (*spawnRequest, error) {
	prompt, _ := body["prompt"].(string)
	if prompt == "" {
		return nil, fmt.Errorf("missing or invalid prompt")
	}
	cwd, _ := body["cwd"].(string)
	if cwd == "" {
		return nil, fmt.Errorf("missing or invalid cwd")
	}
	if _, err := os.Stat(cwd); err != nil {
		return nil, fmt.Errorf("directory does not exist: %s", cwd)
	}

	resumeSessionID, _ := body["resumeSessionId"].(string)
	if resumeSessionID != "" && !uuidRE.MatchString(resumeSessionID) {
		return nil, fmt.Errorf("invalid sessionId format")
	}

	// Resuming an existing monitored session runs in the cwd that session already
	// uses, so only the sensitive-dir blacklist applies. Fresh spawns must pass
	// the full project-roots allowlist.
	if resumeSessionID != "" {
		if err := m.spawnPolicy.AllowResume(cwd); err != nil {
			return nil, err
		}
	} else if err := m.spawnPolicy.Allow(context.Background(), cwd); err != nil {
		return nil, err
	}

	model, _ := body["model"].(string)
	systemPrompt, _ := body["systemPrompt"].(string)
	if len(systemPrompt) > systemPromptMax {
		systemPrompt = systemPrompt[:systemPromptMax]
	}

	permissionMode, _ := body["permissionMode"].(string)
	if permissionMode == "" {
		permissionMode = "default"
	} else {
		if _, ok := allowedPermissionModes[permissionMode]; !ok {
			return nil, fmt.Errorf("invalid permissionMode")
		}
	}

	projectID, _ := body["projectId"].(string)

	enableChannel, _ := body["enableChannel"].(bool)
	if _, hasChannel := body["enableChannel"]; !hasChannel {
		enableChannel = true // default on
	}

	return &spawnRequest{
		prompt:          prompt,
		cwd:             cwd,
		resumeSessionID: resumeSessionID,
		model:           model,
		systemPrompt:    systemPrompt,
		permissionMode:  permissionMode,
		projectID:       projectID,
		enableChannel:   enableChannel,
	}, nil
}

// resolveSpawner looks up the spawner row when spawnerId is set in the body,
// rejects unsupported adapter types, and hydrates req.model from ModelOverride
// when the caller did not supply one.
func (m *SpawnManager) resolveSpawner(sub string, body map[string]any, req *spawnRequest) (*ent.Spawner, error) {
	spawnerID, ok := body["spawnerId"].(string)
	if !ok || spawnerID == "" {
		return nil, nil
	}

	if m.spawnerRepo == nil {
		return nil, fmt.Errorf("spawner not configured")
	}
	row, err := m.spawnerRepo.GetByID(context.Background(), spawnerID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("spawner not found")
		}
		return nil, fmt.Errorf("spawner lookup failed: %w", err)
	}
	switch row.AdapterType {
	case "ollama", "openai":
		return nil, fmt.Errorf("adapter %s not supported for user-initiated spawns; use pipeline tasks instead", row.AdapterType)
	}

	// Hydrate model from override when caller didn't supply one.
	if req.model == "" && row.ModelOverride != nil && *row.ModelOverride != "" {
		req.model = *row.ModelOverride
	}

	return row, nil
}

// buildSpawnArgs assembles the binary path and full argument list for the
// claude process. Order: spawner args first, canonical args last.
func (m *SpawnManager) buildSpawnArgs(req *spawnRequest, spawnerRow *ent.Spawner) (binary string, args []string, err error) {
	binary = claudeBin
	var spawnerArgs []string

	if spawnerRow != nil {
		if ok, reason := services.ValidateSpawnerCommand(spawnerRow.Command); !ok {
			return "", nil, fmt.Errorf("spawner command not permitted: %s", reason)
		}
		if bad := firstReservedFlag(spawnerRow.Args); bad != "" {
			return "", nil, fmt.Errorf("spawner args may not include reserved flag %q", bad)
		}
		if spawnerRow.AdapterType == "custom" {
			binary = spawnerRow.Command
		}
		spawnerArgs = append(spawnerArgs, spawnerRow.Args...)
	}

	var canonicalArgs []string
	if req.resumeSessionID != "" {
		canonicalArgs = append(canonicalArgs, "--resume", req.resumeSessionID)
	}
	canonicalArgs = append(canonicalArgs, "-p", req.prompt)
	if req.model != "" {
		canonicalArgs = append(canonicalArgs, "--model", req.model)
	}
	if req.systemPrompt != "" {
		canonicalArgs = append(canonicalArgs, "--system-prompt", req.systemPrompt)
	}
	// Only append --permission-mode when the resolved spawner has not declared
	// its own permission posture. A spawner that already passes --permission-mode
	// or --dangerously-skip-permissions would otherwise get a second, conflicting
	// flag which causes claude to error.
	if !spawnerArgsControlPermissionMode(spawnerArgs) {
		canonicalArgs = append(canonicalArgs, "--permission-mode", req.permissionMode)
	}

	// Inject --add-dir for additional project folders (multi-folder projects).
	// Only applies to native claude and custom adapters; ollama/openai are already
	// blocked above, so this guard is defence-in-depth for any future adapter types.
	if req.projectID != "" && m.projectFolderRepo != nil &&
		(spawnerRow == nil || spawnerRow.AdapterType == "" || spawnerRow.AdapterType == "claude" || spawnerRow.AdapterType == "custom") {
		folders, lerr := m.projectFolderRepo.ListByProject(context.Background(), req.projectID)
		if lerr != nil {
			slog.Warn("spawn: ListByProject failed; skipping --add-dir injection", "projectId", req.projectID, "err", lerr)
		} else {
			for _, dir := range services.AdditionalDirsForProject(folders, req.cwd) {
				canonicalArgs = append(canonicalArgs, "--add-dir", dir)
			}
		}
	}

	// Order: spawner args first, canonical args last so user-supplied flags win.
	args = append(spawnerArgs, canonicalArgs...)
	return binary, args, nil
}

// watchProcess drains stderr, waits for exit, updates status, and cleans up.
func (m *SpawnManager) watchProcess(cmd *exec.Cmd, status *SpawnStatus, stderrPipe io.ReadCloser, channelCfgPath string) {
	if stderrPipe != nil {
		buf := make([]byte, 1024)
		for {
			n, err := stderrPipe.Read(buf)
			if n > 0 {
				m.mu.Lock()
				status.Stderr += string(buf[:n])
				if len(status.Stderr) > maxStderrBytes {
					status.Stderr = status.Stderr[len(status.Stderr)-maxStderrBytes:]
				}
				m.mu.Unlock()
			}
			if err != nil {
				break
			}
		}
	}
	// Wait for process exit.
	if err := cmd.Wait(); err != nil {
		m.mu.Lock()
		status.Status = "error"
		status.Stderr += "\n" + err.Error()
		m.mu.Unlock()
	} else {
		code := cmd.ProcessState.ExitCode()
		m.mu.Lock()
		status.Status = "exited"
		status.ExitCode = &code
		m.mu.Unlock()
	}
	if channelCfgPath != "" {
		_ = os.Remove(channelCfgPath)
	}
	// Prune old entries.
	m.mu.Lock()
	for k, s := range m.spawnStore {
		t, err := time.Parse(time.RFC3339, s.StartedAt)
		if err == nil && time.Since(t) > spawnStoreMaxAge {
			delete(m.spawnStore, k)
		}
	}
	m.mu.Unlock()
}

// Spawn validates the request, spawns a claude process, and returns the PID.
// sub identifies the requesting user (JWT sub claim). Pass "__global__" in bypass-auth mode.
func (m *SpawnManager) Spawn(sub string, body map[string]any) (int, error) {
	m.mu.Lock()
	m.recordAttempt(sub)
	m.mu.Unlock()

	req, err := m.enforceSpawnPolicy(body)
	if err != nil {
		return 0, err
	}

	spawnerRow, err := m.resolveSpawner(sub, body, req)
	if err != nil {
		return 0, err
	}

	if req.projectID != "" {
		var spawnerID string
		if spawnerRow != nil {
			spawnerID = spawnerRow.ID
		}
		slog.Info("spawn: projectId attached", "sub", sub, "projectId", req.projectID, "spawnerId", spawnerID)
	}

	binary, args, err := m.buildSpawnArgs(req, spawnerRow)
	if err != nil {
		return 0, err
	}

	var channelCfgPath string
	if req.enableChannel {
		selfBin, selfErr := channelconfig.SelfBinaryPath()
		if selfErr != nil {
			slog.Warn("spawn: channel disabled — cannot resolve self binary", "err", selfErr)
		} else if cfgPath, cfgErr := channelconfig.WriteTempConfig(selfBin); cfgErr != nil {
			slog.Warn("spawn: channel disabled — cannot write MCP config", "err", cfgErr)
		} else {
			channelCfgPath = cfgPath
			channelArg := "--mcp-config"
			if spawnerRow != nil && spawnerRow.AdapterType == "custom" {
				if v, ok := spawnerRow.AdapterConfig["channel_arg"]; ok && v != "" {
					channelArg = v
				}
			}
			args = append(args, channelArg, cfgPath)
		}
	}

	cmd := exec.Command(binary, args...)
	cmd.Dir = req.cwd
	cmd.Env = resolveSpawnEnv(spawnerRow)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdin = nil
	cmd.Stdout = nil
	stderrPipe, _ := cmd.StderrPipe()

	if err := execStart(cmd); err != nil {
		if channelCfgPath != "" {
			_ = os.Remove(channelCfgPath)
		}
		return 0, fmt.Errorf("spawn failed: %w", err)
	}

	pid := cmd.Process.Pid
	status := &SpawnStatus{
		PID:       pid,
		Status:    "running",
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		Prompt:    req.prompt[:min(len(req.prompt), 200)],
		Cwd:       req.cwd,
	}
	if spawnerRow != nil {
		status.SpawnerID = spawnerRow.ID
	}

	m.mu.Lock()
	m.spawnStore[pid] = status
	m.mu.Unlock()

	go m.watchProcess(cmd, status, stderrPipe, channelCfgPath)

	return pid, nil
}

// StartPruner starts a background goroutine that prunes spawnStore and userAttempts
// entries older than spawnStoreMaxAge. Returns when ctx is cancelled.
func (m *SpawnManager) StartPruner(ctx context.Context) {
	ticker := time.NewTicker(spawnStoreMaxAge / 2)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.mu.Lock()
			now := time.Now()
			for k, s := range m.spawnStore {
				t, err := time.Parse(time.RFC3339, s.StartedAt)
				if err == nil && now.Sub(t) > spawnStoreMaxAge {
					delete(m.spawnStore, k)
				}
			}
			cutoff := now.Add(-m.rateLimitWindow)
			for sub, attempts := range m.userAttempts {
				i := 0
				for i < len(attempts) && attempts[i].Before(cutoff) {
					i++
				}
				if i == len(attempts) {
					delete(m.userAttempts, sub)
				} else if i > 0 {
					m.userAttempts[sub] = attempts[i:]
				}
			}
			m.mu.Unlock()
		case <-ctx.Done():
			return
		}
	}
}

// GetStatus returns the status of a spawned agent by PID, or nil if unknown.
func (m *SpawnManager) GetStatus(pid int) *SpawnStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.spawnStore[pid]
	if s == nil {
		return nil
	}
	// Return a copy to avoid data races on the caller side.
	cp := *s
	return &cp
}

// SendMessageToChannel forwards a message to the running interactive Claude
// session identified by pid. It consults two discovery files written by the
// pty-broker and channel-bridge respectively, applying the following delivery
// precedence so that the most reliable path is always preferred:
//
//  1. Bridge file ({pid}.json) with a non-empty tmuxPane → tmux send-keys
//     (most reliable; the multiplexer owns the pty).
//  2. Pty file ({pid}.pty.json) with a non-zero port → POST to the pty-broker
//     HTTP endpoint (loopback; the broker owns the pty master).
//  3. Bridge file ({pid}.json) with a non-zero port → POST to the channel-bridge
//     HTTP endpoint (MCP log channel; legacy / pipeline-agent path).
//
// If none of the files are present or usable, an error is returned.
func (m *SpawnManager) SendMessageToChannel(ctx context.Context, pid int, message string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("UserHomeDir: %w", err)
	}
	base := filepath.Join(home, channelconfig.DiscoveryDir, strconv.Itoa(pid))

	// Attempt 1: read the bridge file for tmux delivery.
	var bridgePort int
	var bridgeToken string
	if data, rerr := os.ReadFile(base + ".json"); rerr == nil {
		var disc struct {
			Port       int    `json:"port"`
			Token      string `json:"token"`
			TmuxPane   string `json:"tmuxPane"`
			TmuxSocket string `json:"tmuxSocket"`
		}
		if json.Unmarshal(data, &disc) == nil {
			if disc.TmuxPane != "" {
				// Highest-priority path: tmux send-keys.
				return sendKeysToTmux(ctx, disc.TmuxSocket, disc.TmuxPane, message)
			}
			bridgePort = disc.Port
			bridgeToken = disc.Token
		}
	}

	// Attempt 2: read the pty file for loopback-HTTP delivery.
	if data, rerr := os.ReadFile(base + ".pty.json"); rerr == nil {
		var disc struct {
			Port  int    `json:"port"`
			Token string `json:"token"`
		}
		if json.Unmarshal(data, &disc) == nil && disc.Port != 0 {
			return sendHTTPMessage(ctx, disc.Port, disc.Token, message)
		}
	}

	// Attempt 3: fall back to the bridge HTTP endpoint (legacy/MCP-log path).
	if bridgePort != 0 {
		return sendHTTPMessage(ctx, bridgePort, bridgeToken, message)
	}

	return fmt.Errorf("channel not available for PID %d", pid)
}

// sendHTTPMessage POSTs message to http://127.0.0.1:{port}/message authenticated
// with token as a Bearer credential. Shared by the pty-inject and bridge-HTTP paths.
func sendHTTPMessage(ctx context.Context, port int, token, message string) error {
	body, _ := json.Marshal(map[string]string{"message": message})
	req, err := http.NewRequestWithContext(
		ctx,
		"POST",
		fmt.Sprintf("http://127.0.0.1:%d/message", port),
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: channelMsgTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("channel unreachable: %w", err)
	}
	defer resp.Body.Close()
	if !httputil.Is2xx(resp.StatusCode) {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("channel error %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// SpawnHandler handles spawn-related HTTP endpoints.
type SpawnHandler struct {
	manager *SpawnManager
}

// NewSpawnHandler creates a SpawnHandler backed by the given manager.
func NewSpawnHandler(manager *SpawnManager) *SpawnHandler {
	return &SpawnHandler{manager: manager}
}

// Spawn handles POST /api/agents/spawn.
func (h *SpawnHandler) Spawn(w http.ResponseWriter, r *http.Request) {
	// Extract user identity for per-user rate limiting.
	sub := "__global__"
	if payload, ok := auth.PayloadFromContext(r.Context()); ok && payload.Sub != "" {
		sub = payload.Sub
	}

	if !h.manager.IsSpawnAllowed(sub) {
		http.Error(w, fmt.Sprintf(`{"error":"Too many spawn requests. Max %d per %ds."}`,
			h.manager.rateLimitMax,
			int(h.manager.rateLimitWindow.Seconds()),
		), http.StatusTooManyRequests)
		return
	}

	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	pid, err := h.manager.Spawn(sub, body)
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, services.ErrCwdBlacklisted) || errors.Is(err, services.ErrCwdNotAllowed) {
			slog.Warn("spawn rejected", "cwd", body["cwd"], "user", sub, "reason", err.Error())
			code = http.StatusForbidden
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "pid": pid})
}

// Status handles GET /api/agents/spawn/{pid}/status.
func (h *SpawnHandler) Status(w http.ResponseWriter, r *http.Request) {
	pidStr := r.PathValue("pid")
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		http.Error(w, `{"error":"invalid pid"}`, http.StatusBadRequest)
		return
	}
	status := h.manager.GetStatus(pid)
	if status == nil {
		http.Error(w, `{"error":"unknown spawn PID"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

// Message handles POST /api/agents/{pid}/message.
func (h *SpawnHandler) Message(w http.ResponseWriter, r *http.Request) {
	pidStr := r.PathValue("pid")
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		http.Error(w, `{"error":"invalid pid"}`, http.StatusBadRequest)
		return
	}
	var body struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Message == "" {
		http.Error(w, `{"error":"missing message"}`, http.StatusBadRequest)
		return
	}
	if err := h.manager.SendMessageToChannel(r.Context(), pid, body.Message); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// resolveSpawnEnv builds the child process env per ADR-0003:
//  1. start with os.Environ()
//  2. overlay spawner.Env
//  3. dashboard-controlled vars (DASHBOARD_*, CLAUDE_*) always win
//  4. strip DASHBOARD_JWT_SECRET and DASHBOARD_HOOKS_SECRET
func resolveSpawnEnv(s *ent.Spawner) []string {
	merged := map[string]string{}
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 {
			merged[kv[:i]] = kv[i+1:]
		}
	}
	if s != nil {
		for k, v := range s.Env {
			merged[k] = v
		}
	}
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
	delete(merged, "DASHBOARD_JWT_SECRET")
	delete(merged, "DASHBOARD_HOOKS_SECRET")
	out := make([]string, 0, len(merged))
	for k, v := range merged {
		out = append(out, k+"="+v)
	}
	return out
}

// allowedPermissionModes is the exhaustive set of values the caller may pass
// as body["permissionMode"]. Any other non-empty value is rejected.
var allowedPermissionModes = map[string]struct{}{
	"default":           {},
	"acceptEdits":       {},
	"plan":              {},
	"auto":              {},
	"bypassPermissions": {},
	"dontAsk":           {},
}

// reservedSpawnerFlags are CLI flags the dashboard sets itself; spawner-row
// args must not re-declare them. The list mirrors the canonical args built
// in buildSpawnArgs(): --resume, -p, --model, --system-prompt, --mcp-config.
// --permission-mode is intentionally NOT listed here: spawners may legitimately
// set their own permission posture (handled by spawnerArgsControlPermissionMode).
var reservedSpawnerFlags = map[string]struct{}{
	"--resume":        {},
	"-p":              {},
	"--model":         {},
	"--system-prompt": {},
	"--mcp-config":    {},
}

// firstReservedFlag returns the first arg that names a reserved flag, or "".
// Matches both "--flag" and "--flag=value" forms.
func firstReservedFlag(args []string) string {
	for _, a := range args {
		head := a
		if i := strings.IndexByte(a, '='); i >= 0 {
			head = a[:i]
		}
		if _, bad := reservedSpawnerFlags[head]; bad {
			return head
		}
	}
	return ""
}

// spawnerArgsControlPermissionMode reports whether the provided spawner args
// already declare a permission posture — either an explicit --permission-mode
// (or --permission-mode=value) or one of the two dangerously-skip spellings.
// When true, Spawn must not append its own --permission-mode to avoid a
// duplicate / conflicting flag that would cause claude to error.
func spawnerArgsControlPermissionMode(args []string) bool {
	for _, a := range args {
		if a == "--permission-mode" || strings.HasPrefix(a, "--permission-mode=") {
			return true
		}
		if a == "--dangerously-skip-permissions" || a == "--allow-dangerously-skip-permissions" {
			return true
		}
	}
	return false
}
