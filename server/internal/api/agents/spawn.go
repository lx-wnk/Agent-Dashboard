package agents

import (
	"bytes"
	"context"
	"encoding/json"
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

	"github.com/lx-wnk/agent-dashboard/server/internal/api/spawners"
	"github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/channelconfig"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
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
	rateLimitMax    int
	rateLimitWindow time.Duration
	spawnerRepo     repo.SpawnerRepo

	mu           sync.Mutex
	userAttempts map[string][]time.Time // per-user sliding window keyed by JWT sub (or "__global__" in bypass mode)
	spawnStore   map[int]*SpawnStatus
}

// NewSpawnManager creates a SpawnManager with the given rate limit config.
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

// Spawn validates the request, spawns a claude process, and returns the PID.
// sub identifies the requesting user (JWT sub claim). Pass "__global__" in bypass-auth mode.
func (m *SpawnManager) Spawn(sub string, body map[string]any) (int, error) {
	m.mu.Lock()
	m.recordAttempt(sub)
	m.mu.Unlock()

	prompt, _ := body["prompt"].(string)
	if prompt == "" {
		return 0, fmt.Errorf("missing or invalid prompt")
	}
	cwd, _ := body["cwd"].(string)
	if cwd == "" {
		return 0, fmt.Errorf("missing or invalid cwd")
	}
	if _, err := os.Stat(cwd); err != nil {
		return 0, fmt.Errorf("directory does not exist: %s", cwd)
	}
	if home, err := os.UserHomeDir(); err == nil {
		cwdAbs, _ := filepath.Abs(cwd)
		if real, err := filepath.EvalSymlinks(cwdAbs); err == nil {
			cwdAbs = real
		}
		homeAbs, _ := filepath.Abs(home)
		if !strings.HasPrefix(cwdAbs+string(filepath.Separator), homeAbs+string(filepath.Separator)) {
			return 0, fmt.Errorf("cwd must be within the user home directory")
		}
	}
	model, _ := body["model"].(string)
	systemPrompt, _ := body["systemPrompt"].(string)
	if len(systemPrompt) > systemPromptMax {
		systemPrompt = systemPrompt[:systemPromptMax]
	}
	resumeSessionID, _ := body["resumeSessionId"].(string)
	if resumeSessionID != "" && !uuidRE.MatchString(resumeSessionID) {
		return 0, fmt.Errorf("invalid sessionId format")
	}

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

	projectID, _ := body["projectId"].(string)
	if projectID != "" {
		slog.Info("spawn: projectId attached", "projectId", projectID, "spawnerId", spawnerIDValue(spawnerRow))
	}

	enableChannel, _ := body["enableChannel"].(bool)
	if _, hasChannel := body["enableChannel"]; !hasChannel {
		enableChannel = true // default on
	}

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

	var channelCfgPath string
	if enableChannel {
		selfBin, err := channelconfig.SelfBinaryPath()
		if err != nil {
			slog.Warn("spawn: channel disabled — cannot resolve self binary", "err", err)
		} else if cfgPath, err := channelconfig.WriteTempConfig(selfBin); err != nil {
			slog.Warn("spawn: channel disabled — cannot write MCP config", "err", err)
		} else {
			channelCfgPath = cfgPath
			args = append(args, "--mcp-config", cfgPath)
		}
	}

	cmd := exec.Command(binary, args...)
	cmd.Dir = cwd
	cmd.Env = mergeEnv(spawnerRow)
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
		Prompt:    prompt[:min(len(prompt), 200)],
		Cwd:       cwd,
	}
	if spawnerRow != nil {
		status.SpawnerID = spawnerRow.ID
	}

	m.mu.Lock()
	m.spawnStore[pid] = status
	m.mu.Unlock()

	// Collect stderr in a bounded ring-buffer.
	go func() {
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
	}()

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

// SendMessageToChannel forwards a message to the channel bridge for the given PID.
func (m *SpawnManager) SendMessageToChannel(ctx context.Context, pid int, message string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("UserHomeDir: %w", err)
	}
	path := filepath.Join(home, channelconfig.DiscoveryDir, strconv.Itoa(pid)+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("channel not available for PID %d", pid)
	}
	var disc struct {
		Port  int    `json:"port"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &disc); err != nil || disc.Port == 0 {
		return fmt.Errorf("invalid discovery file for PID %d", pid)
	}

	body, _ := json.Marshal(map[string]string{"message": message})
	req, err := http.NewRequestWithContext(
		ctx,
		"POST",
		fmt.Sprintf("http://127.0.0.1:%d/message", disc.Port),
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+disc.Token)

	client := &http.Client{Timeout: channelMsgTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("channel unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
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
		if strings.HasPrefix(err.Error(), "spawn failed") || strings.HasPrefix(err.Error(), "directory does not exist") {
			code = http.StatusBadRequest
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

// mergeEnv builds the child process env per ADR-0003:
//  1. start with os.Environ()
//  2. overlay spawner.Env
//  3. dashboard-controlled vars (DASHBOARD_*, CLAUDE_*) always win
//  4. strip DASHBOARD_JWT_SECRET and DASHBOARD_HOOKS_SECRET
func mergeEnv(s *ent.Spawner) []string {
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

func spawnerIDValue(s *ent.Spawner) string {
	if s == nil {
		return ""
	}
	return s.ID
}
