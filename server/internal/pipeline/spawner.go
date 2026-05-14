package pipeline

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/channelconfig"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/permissions"
)

var gitPushRE = regexp.MustCompile(`(?i)\bgit push\b`)

// dangerousBashRE matches shell patterns that must never appear in a Bash allow-list entry.
var dangerousBashRE = regexp.MustCompile(
	"(?i)(curl\\b|wget\\b|\\bnc\\b|\\bncat\\b|\\bnetcat\\b|bash\\s+-c|sh\\s+-c|\\beval\\b|python\\s+-c|perl\\s+-e|ruby\\s+-e|base64\\s+-d|\\$\\(|`|&&|\\||;\\s*\\w|>\\s*\\w|<\\s*\\w|chmod\\s+\\+x|rm\\s+-rf|exec\\s+\\w|\\bxargs\\b|find\\s+.*-exec)",
)

const systemPromptMaxChars = 10000

type SpawnAgentOptions struct {
	Task            *ent.Task
	StageRun        *ent.StageRun
	Prompt          string
	SystemPrompt    string
	Model           string
	Permissions     []*ent.TaskPermission
	EnableChannel   bool
	ResumeSessionID string
	MCPToken        string
	MCPUrl          string
}

type SpawnResult struct {
	PID          int
	Cwd          string
	SettingsPath string
	Cleanup      func()
}

var channelAllow = []string{
	"mcp__dashboard-channel__dashboard_reply",
	"mcp__dashboard-channel__request_permission",
}

func BuildAllowList(perms []*ent.TaskPermission, enableChannel, allowGitPush bool) []string {
	var allow []string
	if enableChannel {
		allow = append(allow, channelAllow...)
	}
	now := time.Now()
	for _, p := range perms {
		if !p.Granted {
			continue
		}
		if p.ExpiresAt != nil && p.ExpiresAt.Before(now) {
			continue
		}
		if !permissions.IsAllowedTool(p.Tool) {
			continue
		}
		if !allowGitPush && p.Tool == "Bash" && p.Pattern != nil && gitPushRE.MatchString(*p.Pattern) {
			continue
		}
		if p.Tool == "Bash" {
			if p.Pattern == nil || *p.Pattern == "" {
				continue // blanket Bash allow is forbidden
			}
			normalized := strings.Join(strings.Fields(*p.Pattern), " ")
			if dangerousBashRE.MatchString(normalized) {
				continue // dangerous shell pattern
			}
			allow = append(allow, fmt.Sprintf("Bash(%s)", normalized))
			continue
		}
		if p.Pattern != nil && *p.Pattern != "" {
			allow = append(allow, fmt.Sprintf("%s(%s)", p.Tool, *p.Pattern))
		} else {
			allow = append(allow, p.Tool)
		}
	}
	return allow
}

func BuildSpawnArgs(opts SpawnAgentOptions) []string {
	var args []string
	if opts.ResumeSessionID != "" {
		args = append(args, "--resume", opts.ResumeSessionID)
	}
	args = append(args, "-p", opts.Prompt)
	args = append(args, "--permission-mode", "default")
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.SystemPrompt != "" {
		sp := opts.SystemPrompt
		if len(sp) > systemPromptMaxChars {
			sp = sp[:systemPromptMaxChars]
		}
		args = append(args, "--system-prompt", sp)
	}
	return args
}

func buildSpawnArgsWithChannelConfig(opts SpawnAgentOptions, channelCfgPath string) []string {
	args := BuildSpawnArgs(opts)
	if channelCfgPath != "" {
		args = append(args, "--mcp-config", channelCfgPath)
	}
	return args
}

// allowedEnvPrefixes are env var prefixes always forwarded to spawned agents.
var allowedEnvPrefixes = []string{"CLAUDE_", "DASHBOARD_"}

// deniedEnvKeys are secrets that must never reach spawned agents even if they
// match an allowedEnvPrefixes entry.
var deniedEnvKeys = map[string]struct{}{
	"DASHBOARD_JWT_SECRET":   {},
	"DASHBOARD_HOOKS_SECRET": {},
}

// allowedEnvKeys are exact env var names always forwarded to spawned agents.
var allowedEnvKeys = map[string]struct{}{
	"PATH":            {},
	"HOME":            {},
	"USER":            {},
	"LOGNAME":         {},
	"LANG":            {},
	"LC_ALL":          {},
	"LC_CTYPE":        {},
	"TERM":            {},
	"SHELL":           {},
	"TMPDIR":          {},
	"TMP":             {},
	"TEMP":            {},
	"XDG_RUNTIME_DIR": {},
	"XDG_CONFIG_HOME": {},
	"GOPATH":          {},
	"GOROOT":          {},
	"NODE_PATH":       {},
}

func BuildSpawnEnv(opts SpawnAgentOptions) []string {
	var env []string
	for _, e := range os.Environ() {
		key, _, found := strings.Cut(e, "=")
		if !found {
			continue
		}
		// Deny list takes precedence over both allowedEnvKeys and allowedEnvPrefixes.
		if _, denied := deniedEnvKeys[key]; denied {
			continue
		}
		if _, ok := allowedEnvKeys[key]; ok {
			env = append(env, e)
			continue
		}
		for _, prefix := range allowedEnvPrefixes {
			if strings.HasPrefix(key, prefix) {
				env = append(env, e)
				break
			}
		}
	}
	// Always inject dashboard-specific vars (override any inherited values).
	env = append(env, fmt.Sprintf("DASHBOARD_STAGE_RUN_ID=%s", opts.StageRun.ID))
	env = append(env, fmt.Sprintf("DASHBOARD_TASK_ID=%s", opts.Task.ID))
	if opts.MCPToken != "" {
		env = append(env, fmt.Sprintf("DASHBOARD_MCP_TOKEN=%s", opts.MCPToken))
	}
	if opts.MCPUrl != "" {
		env = append(env, fmt.Sprintf("DASHBOARD_MCP_URL=%s", opts.MCPUrl))
	}
	return env
}

func writeSettingsFile(cwd string, perms []*ent.TaskPermission, enableChannel, allowGitPush bool) (string, bool, bool, error) {
	allow := BuildAllowList(perms, enableChannel, allowGitPush)
	if len(allow) == 0 {
		return "", false, false, nil
	}
	claudeDir := filepath.Join(cwd, ".claude")
	settingsPath := filepath.Join(claudeDir, "settings.json")
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		if err := os.MkdirAll(claudeDir, 0o700); err != nil {
			return "", false, false, fmt.Errorf("writeSettingsFile: mkdir .claude: %w", err)
		}
		settings := map[string]any{
			"permissions":       map[string]any{"allow": allow},
			"_dashboardManaged": true,
		}
		data, _ := json.MarshalIndent(settings, "", "  ")
		if err := os.WriteFile(settingsPath, data, 0o600); err != nil {
			return "", false, false, fmt.Errorf("writeSettingsFile: write: %w", err)
		}
		return settingsPath, true, false, nil
	}
	slog.Warn("settings.json is not dashboard-managed — merging into settings.local.json", "path", settingsPath)
	localPath := filepath.Join(claudeDir, "settings.local.json")
	var existing map[string]any
	if data, err := os.ReadFile(localPath); err == nil {
		_ = json.Unmarshal(data, &existing)
	}
	if existing == nil {
		existing = map[string]any{}
	}
	existingPerms, _ := existing["permissions"].(map[string]any)
	if existingPerms == nil {
		existingPerms = map[string]any{}
	}
	existingAllow, _ := existingPerms["allow"].([]any)
	existingSet := make(map[string]bool, len(existingAllow))
	for _, e := range existingAllow {
		if s, ok := e.(string); ok {
			existingSet[s] = true
		}
	}
	var newEntries []string
	for _, entry := range allow {
		if !existingSet[entry] {
			newEntries = append(newEntries, entry)
		}
	}
	if len(newEntries) == 0 {
		return localPath, false, true, nil
	}
	merged := make([]any, 0, len(existingAllow)+len(newEntries))
	merged = append(merged, existingAllow...)
	for _, e := range newEntries {
		merged = append(merged, e)
	}
	existingPerms["allow"] = merged
	existing["permissions"] = existingPerms
	existing["_dashboardManagedAllows"] = newEntries
	if err := os.MkdirAll(claudeDir, 0o700); err != nil {
		return "", false, false, fmt.Errorf("writeSettingsFile: mkdir .claude (local): %w", err)
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	if err := os.WriteFile(localPath, data, 0o600); err != nil {
		return "", false, false, fmt.Errorf("writeSettingsFile: write local: %w", err)
	}
	return localPath, true, true, nil
}

func ShouldCleanSettingsFile(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return false
	}
	managed, _ := parsed["_dashboardManaged"].(bool)
	return managed
}

// stderrLogger forwards lines written to stderr of a spawned agent to slog.Warn.
type stderrLogger struct{ prefix string }

func (l *stderrLogger) Write(b []byte) (int, error) {
	line := strings.TrimRight(string(b), "\n")
	if line != "" {
		slog.Warn("agent stderr", "prefix", l.prefix, "line", line)
	}
	return len(b), nil
}

func IsGitPushAllowed(t *ent.Task) bool {
	if t.Metadata != nil {
		if v, ok := t.Metadata["allowGitPush"].(bool); ok && v {
			return true
		}
	}
	return os.Getenv("DASHBOARD_ALLOW_GIT_PUSH") == "true"
}

func SpawnStageAgent(opts SpawnAgentOptions) (SpawnResult, error) {
	cwd := opts.Task.Cwd
	if opts.Task.WorktreePath != nil && *opts.Task.WorktreePath != "" {
		cwd = *opts.Task.WorktreePath
	}
	allowGitPush := IsGitPushAllowed(opts.Task)
	settingsPath, wrote, isLocal, err := writeSettingsFile(cwd, opts.Permissions, opts.EnableChannel, allowGitPush)
	if err != nil {
		slog.Warn("writeSettingsFile failed — continuing without pre-approved allow-list", "err", err)
	}

	// Write a temp MCP config file so the spawned agent gets the dashboard-channel MCP server.
	// Failures are non-fatal: the agent runs without the channel bridge rather than refusing to spawn.
	var channelCfgPath string
	if opts.EnableChannel {
		if selfBin, binErr := channelconfig.SelfBinaryPath(); binErr == nil {
			if cfgPath, cfgErr := channelconfig.WriteTempConfig(selfBin); cfgErr == nil {
				channelCfgPath = cfgPath
			} else {
				slog.Warn("channelconfig: failed to write temp config — agent runs without channel bridge", "err", cfgErr)
			}
		} else {
			slog.Warn("channelconfig: failed to resolve self binary path — agent runs without channel bridge", "err", binErr)
		}
	}

	args := buildSpawnArgsWithChannelConfig(opts, channelCfgPath)
	cmd := exec.Command("claude", args...)
	cmd.Dir = cwd
	cmd.Env = BuildSpawnEnv(opts)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = &stderrLogger{prefix: fmt.Sprintf("agent[%s]", opts.Task.Slug)}
	if err := cmd.Start(); err != nil {
		if channelCfgPath != "" {
			_ = os.Remove(channelCfgPath)
		}
		return SpawnResult{}, fmt.Errorf("SpawnStageAgent.Start: %w", err)
	}
	cleanup := func() {
		if channelCfgPath != "" {
			_ = os.Remove(channelCfgPath)
		}
		if !wrote || settingsPath == "" {
			return
		}
		if isLocal {
			cleanupLocalSettingsEntries(settingsPath)
		} else if ShouldCleanSettingsFile(settingsPath) {
			_ = os.Remove(settingsPath)
		}
	}
	return SpawnResult{PID: cmd.Process.Pid, Cwd: cwd, SettingsPath: settingsPath, Cleanup: cleanup}, nil
}

func cleanupLocalSettingsEntries(localPath string) {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return
	}
	managed, _ := parsed["_dashboardManagedAllows"].([]any)
	if len(managed) == 0 {
		return
	}
	managedSet := make(map[string]bool, len(managed))
	for _, e := range managed {
		if s, ok := e.(string); ok {
			managedSet[s] = true
		}
	}
	delete(parsed, "_dashboardManagedAllows")
	if perms, ok := parsed["permissions"].(map[string]any); ok {
		if allow, ok := perms["allow"].([]any); ok {
			var filtered []any
			for _, e := range allow {
				if s, ok := e.(string); !ok || !managedSet[s] {
					filtered = append(filtered, e)
				}
			}
			if len(filtered) == 0 {
				delete(perms, "allow")
			} else {
				perms["allow"] = filtered
			}
			if len(perms) == 0 {
				delete(parsed, "permissions")
			}
		}
	}
	if len(parsed) == 0 {
		_ = os.Remove(localPath)
		return
	}
	out, _ := json.MarshalIndent(parsed, "", "  ")
	_ = os.WriteFile(localPath, out, 0o600)
}
