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
	"github.com/lx-wnk/agent-dashboard/server/internal/pathutil"
	"github.com/lx-wnk/agent-dashboard/server/internal/permissions"
	"github.com/lx-wnk/agent-dashboard/server/internal/taskcontrol"
)

var gitPushRE = regexp.MustCompile(`(?i)\bgit push\b`)

// syntheticSpawnPID is the PID returned by syntheticSpawn. No real process
// owns this value, so IsPidAlive is false and syscallKill is a no-op.
const syntheticSpawnPID = 2147483647

// syntheticSpawn is the default SpawnFunc used when no real spawner is wired
// (all tests, the package-level HandlersByStage registry). It returns a
// SpawnResult whose PID no real process owns, so IsPidAlive is false and
// syscallKill is a harmless no-op.
func syntheticSpawn(opts SpawnAgentOptions) (SpawnResult, error) {
	cwd := opts.Task.Cwd
	if opts.Task.WorktreePath != nil && *opts.Task.WorktreePath != "" {
		cwd = *opts.Task.WorktreePath
	}
	return SpawnResult{PID: syntheticSpawnPID, Cwd: cwd, Cleanup: func() {}}, nil
}

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
	// Spawner is the resolved DB spawner row that controls which executable
	// is launched and which extra env vars are seeded. When nil the spawner
	// behaves identically to the legacy `claude` CLI path. The built-in
	// claude-default spawner (Command="claude", empty Args/Env) is also
	// treated as the legacy path so existing tasks spawn byte-identically.
	Spawner *ent.Spawner

	// AdditionalDirs holds the paths of project folders (beyond the cwd) that
	// should be accessible to the spawned agent. Each non-empty entry is passed
	// to the `claude` CLI as `--add-dir <path>`. The default folder is already
	// the cwd and must NOT appear here.
	AdditionalDirs []string

	// AllowGitPush is the global git-push setting ("git.allowPush"). Combined with
	// the per-task metadata override inside IsGitPushAllowed.
	AllowGitPush bool
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

// BuildDenyList returns the settings deny entries that must be written
// alongside the allow list. On the allow-all path, a deny for "Bash(git push:*)"
// is injected when allowGitPush is false so Claude refuses git-push even though
// blanket Bash is allowed. Returns nil when no deny entries are needed.
func BuildDenyList(autonomy string, allowGitPush bool) []string {
	if taskcontrol.IsAllowAll(autonomy) && !allowGitPush {
		return []string{"Bash(git push:*)"}
	}
	return nil
}

// BuildAllowList assembles the --allowedTools slice for a spawn.
// When autonomy is an allow-all level (spec_gated or full), the restrictive
// per-permission loop is skipped and the permissive list is returned instead.
func BuildAllowList(autonomy string, perms []*ent.TaskPermission, enableChannel, allowGitPush bool) []string {
	var allow []string
	if enableChannel {
		allow = append(allow, channelAllow...)
	}
	if taskcontrol.IsAllowAll(autonomy) {
		return append(allow, taskcontrol.PermissiveAllowList(allowGitPush)...)
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
		if p.Tool == "Bash" {
			if p.Pattern == nil || *p.Pattern == "" {
				continue // blanket Bash allow is forbidden
			}
			normalized := strings.Join(strings.Fields(*p.Pattern), " ")
			if p.ManualOverride {
				// Human explicitly approved this exact pattern — skip allow-list and git-push gate.
				allow = append(allow, fmt.Sprintf("Bash(%s)", normalized))
				continue
			}
			if !allowGitPush && gitPushRE.MatchString(normalized) {
				continue
			}
			if ok, _ := permissions.IsSafeBashPattern(normalized); !ok {
				continue // unsafe shell pattern — skip silently at spawn time
			}
			allow = append(allow, fmt.Sprintf("Bash(%s)", normalized))
			continue
		}
		if p.Tool == "WebFetch" {
			// Bare WebFetch grants (no pattern) are rejected — require a domain pattern.
			if p.Pattern == nil || strings.TrimSpace(*p.Pattern) == "" {
				continue
			}
			allow = append(allow, fmt.Sprintf("WebFetch(%s)", strings.TrimSpace(*p.Pattern)))
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
	// Only force --permission-mode default when the spawner has not declared its
	// own permission posture. A spawner that passes --dangerously-skip-permissions
	// or its own --permission-mode (e.g. "auto"/"acceptEdits") would otherwise get
	// a SECOND, conflicting --permission-mode default appended — claude then errors
	// on the duplicate flag (or default silently wins), putting the agent back into
	// a gated mode with an (often empty) allow-list where every Edit/Write/Bash fails.
	if !spawnerControlsPermissionMode(opts.Spawner) {
		args = append(args, "--permission-mode", "default")
	}
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
	// Apply spawner ModelOverride only when no --model was already injected
	// from opts.Model. The override is meant for spawners that target a
	// fixed model regardless of the task's preferred model.
	if opts.Spawner != nil && opts.Spawner.ModelOverride != nil && *opts.Spawner.ModelOverride != "" {
		if !containsArg(args, "--model") {
			args = append(args, "--model", *opts.Spawner.ModelOverride)
		}
	}
	// Grant the spawned agent access to extra project folders. Each path is
	// passed as a separate --add-dir flag so the claude CLI adds the directory
	// to its allowed file-system scope. Empty entries are skipped defensively.
	for _, dir := range opts.AdditionalDirs {
		if dir == "" {
			continue
		}
		args = append(args, "--add-dir", dir)
	}
	return args
}

// skipPermissionFlags are the claude CLI flags that bypass all permission
// checks. Both spellings are accepted by the CLI.
var skipPermissionFlags = map[string]struct{}{
	"--dangerously-skip-permissions":       {},
	"--allow-dangerously-skip-permissions": {},
}

// spawnerControlsPermissionMode reports whether the resolved spawner already
// declares its own permission posture — either a --dangerously-skip-permissions
// flag (either spelling) or an explicit --permission-mode. When true, the
// dashboard must not append its default --permission-mode, to avoid a duplicate
// / conflicting flag.
func spawnerControlsPermissionMode(sp *ent.Spawner) bool {
	if sp == nil {
		return false
	}
	for _, a := range sp.Args {
		if _, ok := skipPermissionFlags[a]; ok {
			return true
		}
		if a == "--permission-mode" || strings.HasPrefix(a, "--permission-mode=") {
			return true
		}
	}
	return false
}

func containsArg(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

// buildExecCommand returns an *exec.Cmd configured for the resolved spawner.
// Spawner-declared args appear BEFORE dashboard-built args so interpreter
// flags (e.g. `npx` + package name) precede the per-task switches.
func buildExecCommand(sp *ent.Spawner, args []string) *exec.Cmd {
	if isLegacyClaudeSpawner(sp) {
		return exec.Command("claude", args...)
	}
	combined := make([]string, 0, len(sp.Args)+len(args))
	combined = append(combined, sp.Args...)
	combined = append(combined, args...)
	return exec.Command(sp.Command, combined...)
}

// isLegacyClaudeSpawner returns true when the spawner is nil or describes the
// built-in `claude` CLI with no custom args. In that case SpawnStageAgent
// behaves byte-identically to the pre-pluggable-spawner path.
func isLegacyClaudeSpawner(sp *ent.Spawner) bool {
	if sp == nil {
		return true
	}
	if (sp.Command == "" || sp.Command == "claude") && len(sp.Args) == 0 {
		return true
	}
	return false
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

// expandLeadingTilde is an alias for pathutil.ExpandLeadingTilde kept for
// package-internal use. Callers outside pipeline should import pathutil directly.
var expandLeadingTilde = pathutil.ExpandLeadingTilde

func BuildSpawnEnv(opts SpawnAgentOptions) []string {
	// Stage 1: spawner-declared env (lowest precedence). Each entry is
	// subject to the global deny list before reaching the merged map.
	merged := make(map[string]string)
	if opts.Spawner != nil {
		for k, v := range opts.Spawner.Env {
			if _, denied := deniedEnvKeys[k]; denied {
				continue
			}
			// Spawner env is user-authored config, not a shell expansion, so a
			// leading `~` is taken literally by exec — `CLAUDE_CONFIG_DIR=~/x`
			// would make claude create a bogus `./~/x` dir under the cwd. Expand
			// it the way a shell would before forwarding.
			merged[k] = expandLeadingTilde(v)
		}
	}

	// Stage 2: inherited process env, filtered by the allow-list/prefix
	// rules. Always wins over a spawner-declared key of the same name.
	for _, e := range os.Environ() {
		key, val, found := strings.Cut(e, "=")
		if !found {
			continue
		}
		if _, denied := deniedEnvKeys[key]; denied {
			continue
		}
		if _, ok := allowedEnvKeys[key]; ok {
			merged[key] = val
			continue
		}
		for _, prefix := range allowedEnvPrefixes {
			if strings.HasPrefix(key, prefix) {
				merged[key] = val
				break
			}
		}
	}

	// Stage 3: dashboard-controlled identifiers (highest precedence).
	merged["DASHBOARD_STAGE_RUN_ID"] = opts.StageRun.ID
	merged["DASHBOARD_TASK_ID"] = opts.Task.ID
	if opts.MCPToken != "" {
		merged["DASHBOARD_MCP_TOKEN"] = opts.MCPToken
	}
	if opts.MCPUrl != "" {
		merged["DASHBOARD_MCP_URL"] = opts.MCPUrl
	}

	// Final defense-in-depth pass: secrets must never leak even if a future
	// code path puts them into `merged` above. The Stage-1 and Stage-2 loops
	// already filter them, but this guarantees the invariant at the exit.
	for denied := range deniedEnvKeys {
		delete(merged, denied)
	}

	env := make([]string, 0, len(merged))
	for k, v := range merged {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	return env
}

func writeSettingsFile(autonomy, cwd string, perms []*ent.TaskPermission, enableChannel, allowGitPush bool) (string, bool, bool, error) {
	allow := BuildAllowList(autonomy, perms, enableChannel, allowGitPush)
	deny := BuildDenyList(autonomy, allowGitPush)
	if len(allow) == 0 && len(deny) == 0 {
		return "", false, false, nil
	}
	claudeDir := filepath.Join(cwd, ".claude")
	settingsPath := filepath.Join(claudeDir, "settings.json")
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		if err := os.MkdirAll(claudeDir, 0o700); err != nil {
			return "", false, false, fmt.Errorf("writeSettingsFile: mkdir .claude: %w", err)
		}
		permsMap := map[string]any{"allow": allow}
		if len(deny) > 0 {
			permsMap["deny"] = deny
		}
		settings := map[string]any{
			"permissions":       permsMap,
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
	// Union deny entries the same way allow entries are unioned.
	existingDeny, _ := existingPerms["deny"].([]any)
	existingDenySet := make(map[string]bool, len(existingDeny))
	for _, e := range existingDeny {
		if s, ok := e.(string); ok {
			existingDenySet[s] = true
		}
	}
	var newDenyEntries []string
	for _, entry := range deny {
		if !existingDenySet[entry] {
			newDenyEntries = append(newDenyEntries, entry)
		}
	}
	if len(newEntries) == 0 && len(newDenyEntries) == 0 {
		return localPath, false, true, nil
	}
	merged := make([]any, 0, len(existingAllow)+len(newEntries))
	merged = append(merged, existingAllow...)
	for _, e := range newEntries {
		merged = append(merged, e)
	}
	existingPerms["allow"] = merged
	if len(newDenyEntries) > 0 {
		mergedDeny := make([]any, 0, len(existingDeny)+len(newDenyEntries))
		mergedDeny = append(mergedDeny, existingDeny...)
		for _, e := range newDenyEntries {
			mergedDeny = append(mergedDeny, e)
		}
		existingPerms["deny"] = mergedDeny
	}
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

// IsGitPushAllowed reports whether the task may run `git push`: true when the
// task opts in via metadata["allowGitPush"], or when the global setting
// (git.allowPush) is enabled.
func IsGitPushAllowed(t *ent.Task, allowGitPushGlobal bool) bool {
	if t.Metadata != nil {
		if v, ok := t.Metadata["allowGitPush"].(bool); ok && v {
			return true
		}
	}
	return allowGitPushGlobal
}

func SpawnStageAgent(opts SpawnAgentOptions) (SpawnResult, error) {
	cwd := opts.Task.Cwd
	if opts.Task.WorktreePath != nil && *opts.Task.WorktreePath != "" {
		cwd = *opts.Task.WorktreePath
	}
	allowGitPush := IsGitPushAllowed(opts.Task, opts.AllowGitPush)
	settingsPath, wrote, isLocal, err := writeSettingsFile(opts.Task.Autonomy, cwd, opts.Permissions, opts.EnableChannel, allowGitPush)
	if err != nil {
		if !taskcontrol.IsAllowAll(opts.Task.Autonomy) {
			return SpawnResult{}, fmt.Errorf("writeSettingsFile: %w", err)
		}
		// Allow-all autonomy needs no allow-list file — a write failure here is harmless.
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
	cmd := buildExecCommand(opts.Spawner, args)
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
