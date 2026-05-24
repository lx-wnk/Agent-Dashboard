package parser

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lx-wnk/agent-dashboard/sdk"
)

const tailBytes = 32768 // 32KB from end

var (
	uuidRE  = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	quotaRE = regexp.MustCompile(`(?i)quota exceeded|usage limit|monthly limit`)
	rateRE  = regexp.MustCompile(`(?i)rate limit|429|too many requests|throttl`)
	authRE  = regexp.MustCompile(`(?i)invalid api key|authentication|unauthorized|401`)
)

// SessionCacheTTL is the maximum age of a cached FindSessionForProject result.
// Set to the SSE broadcast interval (default 3 s) so each tick re-uses the
// cached parse instead of tail-reading every JSONL file again.
// The merger package raises this at startup when a non-default interval is configured.
var SessionCacheTTL = 3 * time.Second

// sessionCacheKey identifies a cached parse result.
type sessionCacheKey struct {
	cwd       string
	configDir string
}

// sessionCacheEntry holds a cached result keyed by the winning file's identity.
type sessionCacheEntry struct {
	// file identity — used to detect changes without re-reading content
	path  string
	inode uint64
	mtime time.Time
	// cached parse output
	data *SessionData
	// wall-clock time when this entry was stored
	cachedAt time.Time
}

var (
	sessionCacheMu sync.Mutex
	sessionCache   = make(map[sessionCacheKey]*sessionCacheEntry)
)

// claudeConfigDir returns the Claude config base directory.
// Respects CLAUDE_CONFIG_DIR env var; falls back to ~/.claude.
func claudeConfigDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

// allClaudeConfigDirs returns all candidate Claude config directories to search.
// Priority order:
//  1. DASHBOARD_CLAUDE_CONFIG_DIRS — explicit comma-separated list (highest priority)
//  2. CLAUDE_CONFIG_DIR from the server process environment
//  3. Default ~/.claude
//  4. Common custom variants that exist on disk (~/.claude-personal, etc.)
func allClaudeConfigDirs() []string {
	seen := make(map[string]bool)
	var dirs []string
	add := func(d string) {
		d = strings.TrimSpace(d)
		if d != "" && !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	// 1. Explicit dashboard config — overrides auto-detection for teams/multi-user setups.
	if val := os.Getenv("DASHBOARD_CLAUDE_CONFIG_DIRS"); val != "" {
		for _, p := range strings.Split(val, ",") {
			add(p)
		}
	}
	// 2. Server process CLAUDE_CONFIG_DIR.
	add(claudeConfigDir())
	// 3. Standard default.
	home, _ := os.UserHomeDir()
	add(filepath.Join(home, ".claude"))
	// 4. Common custom dir names that exist on disk.
	for _, name := range []string{".claude-personal", ".claude-work", ".claude-dev"} {
		candidate := filepath.Join(home, name)
		if _, err := os.Stat(filepath.Join(candidate, "projects")); err == nil {
			add(candidate)
		}
	}
	return dirs
}

// claudeProjectsDir returns the Claude projects directory.
func claudeProjectsDir() string {
	return filepath.Join(claudeConfigDir(), "projects")
}

// AllClaudeConfigDirs returns all Claude config directories to search.
// Exported for use by packages that need JSONL file discovery (e.g., analytics).
// Deprecated: use AllAgentConfigDirs for multi-provider discovery.
func AllClaudeConfigDirs() []string {
	return allClaudeConfigDirs()
}

// ProviderConfigDir associates a provider identifier with a config directory path.
type ProviderConfigDir struct {
	Provider sdk.Provider
	Path     string
}

// AllAgentConfigDirs returns config directories for all supported providers.
// Claude directories are listed first; Codex (~/.codex or CODEX_HOME) and
// Gemini (~/.gemini) are appended when present on disk.
// Missing directories are silently skipped — no error is returned.
func AllAgentConfigDirs() []ProviderConfigDir {
	home, _ := os.UserHomeDir()
	var result []ProviderConfigDir

	// Claude: all configured dirs
	for _, d := range allClaudeConfigDirs() {
		result = append(result, ProviderConfigDir{Provider: sdk.ProviderClaude, Path: d})
	}

	// Codex: ~/.codex or $CODEX_HOME
	// Session structure: ~/.codex/projects/{encoded}/{sessionId}.jsonl  (best-guess, research-confirmed absent)
	// If CODEX_HOME is set, prefer it; otherwise fall back to ~/.codex.
	// Note: as of 2026-05, the Codex CLI (openai/codex) does not yet write JSONL
	// session logs; this detection is a forward-compatible stub that will activate
	// once Codex supports local session persistence.
	codexDir := os.Getenv("CODEX_HOME")
	if codexDir == "" {
		codexDir = filepath.Join(home, ".codex")
	}
	if dirExists(codexDir) {
		result = append(result, ProviderConfigDir{Provider: sdk.ProviderCodex, Path: codexDir})
	}

	// Gemini CLI: ~/.gemini
	// Session structure: ~/.gemini/projects/{encoded}/{sessionId}.jsonl  (best-guess)
	// Note: as of 2026-05, the Gemini CLI (google-gemini/gemini-cli) stores sessions
	// under ~/.gemini/tmp/{sessionId}/ as markdown, not JSONL. This stub activates
	// if/when Gemini CLI adopts JSONL-compatible session logs.
	geminiDir := filepath.Join(home, ".gemini")
	if dirExists(geminiDir) {
		result = append(result, ProviderConfigDir{Provider: sdk.ProviderGemini, Path: geminiDir})
	}

	return result
}

// dirExists reports whether path exists and is a directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// ClaudeProjectsDir returns the Claude projects directory — exported for use by pipeline package.
func ClaudeProjectsDir() string {
	return claudeProjectsDir()
}

// sessionMetaDir returns the Claude session-meta directory.
func sessionMetaDir() string {
	return filepath.Join(claudeConfigDir(), "usage-data", "session-meta")
}

// TailRead reads the last tailBytes bytes of a file.
// The first line of the result may be truncated (partial JSON) and should be skipped.
func TailRead(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", filePath, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", filePath, err)
	}
	size := info.Size()
	readSize := int64(tailBytes)
	if readSize > size {
		readSize = size
	}
	if _, err := f.Seek(size-readSize, io.SeekStart); err != nil {
		return "", fmt.Errorf("seek: %w", err)
	}
	buf := make([]byte, readSize)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", fmt.Errorf("read: %w", err)
	}
	return string(buf[:n]), nil
}

// jsonlMessage is the minimal structure of a JSONL session log entry.
type jsonlMessage struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"` // ISO 8601, e.g. "2025-01-15T10:30:00.000Z"
	Message   json.RawMessage `json:"message"`
}

type msgContent struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	Model   string          `json:"model"`
	Usage   *struct {
		InputTokens         int `json:"input_tokens"`
		OutputTokens        int `json:"output_tokens"`
		CacheCreationTokens int `json:"cache_creation_input_tokens"`
		CacheReadTokens     int `json:"cache_read_input_tokens"`
	} `json:"usage"`
}

type toolUseBlock struct {
	Type  string          `json:"type"`
	Name  string          `json:"name"`
	Text  string          `json:"text"`
	Input json.RawMessage `json:"input"`
}

// todoInput is the input shape for TodoWrite tool calls.
type todoInput struct {
	Todos []struct {
		ID      string `json:"id"`
		Content string `json:"content"`
		Status  string `json:"status"`
	} `json:"todos"`
}

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

// sessionFileCandidate holds mtime + inode info gathered via os.Stat (cheap).
type sessionFileCandidate struct {
	path  string
	mtime time.Time
	inode uint64
}

// statSessionFiles lists JSONL session files in projectDir ordered by mtime desc.
// Only os.Stat is called — no file content is read.
func statSessionFiles(projectDir string) ([]sessionFileCandidate, error) {
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return nil, fmt.Errorf("readdir %s: %w", projectDir, err)
	}
	var out []sessionFileCandidate
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		id := strings.TrimSuffix(name, ".jsonl")
		if !uuidRE.MatchString(id) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, sessionFileCandidate{
			path:  filepath.Join(projectDir, name),
			mtime: info.ModTime(),
			inode: inodeOf(info),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].mtime.After(out[j].mtime)
	})
	return out, nil
}

// FindSessionForProject locates the most recently active JSONL session for cwd.
// claudeConfigDir, if non-empty, overrides the default ~/.claude config directory
// (use the value of CLAUDE_CONFIG_DIR from the process environment).
//
// Results are cached per (cwd, configDir) keyed by the winning file's path,
// inode, and mtime.  A cached result is reused when:
//   - the same file is still the newest JSONL in the project directory, AND
//   - its inode and mtime are unchanged (no write happened), AND
//   - the entry was stored within SessionCacheTTL.
//
// This eliminates repeated 32 KB tail-reads per SSE tick on long session histories.
func FindSessionForProject(cwd string, uptimeSeconds int64, claudeConfigDir string) (*SessionData, error) {
	baseDir := claudeProjectsDir()
	if claudeConfigDir != "" {
		baseDir = filepath.Join(claudeConfigDir, "projects")
	}
	encoded := EncodePath(cwd)
	projectDir := filepath.Join(baseDir, encoded)

	candidates, err := statSessionFiles(projectDir)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no session files in %s", projectDir)
	}

	cacheKey := sessionCacheKey{cwd: cwd, configDir: claudeConfigDir}
	now := time.Now()

	// Check cache against the top candidate (most recently modified file).
	top := candidates[0]
	sessionCacheMu.Lock()
	entry := sessionCache[cacheKey]
	sessionCacheMu.Unlock()

	if entry != nil &&
		now.Sub(entry.cachedAt) < SessionCacheTTL &&
		entry.path == top.path &&
		entry.inode == top.inode &&
		entry.mtime.Equal(top.mtime) {
		// Cache hit: return a shallow copy so callers can safely mutate fields.
		cp := *entry.data
		return &cp, nil
	}

	// Cache miss — fall back to full scan with content reads.
	data, chosenPath, err := findSessionByContent(candidates, uptimeSeconds, cwd)
	if err != nil {
		return nil, err
	}

	// Populate session ID and meta before caching.
	data.SessionID = strings.TrimSuffix(filepath.Base(chosenPath), ".jsonl")
	data.ProjectPath = cwd
	data.Meta = loadSessionMeta(data.SessionID)

	// Store in cache keyed by the winning file's identity.
	// We always cache against the top (newest-mtime) candidate so that when
	// the active session file advances (new write → mtime bump) we get a cache
	// miss on the next tick rather than returning stale data.
	newEntry := &sessionCacheEntry{
		path:     top.path,
		inode:    top.inode,
		mtime:    top.mtime,
		data:     data,
		cachedAt: now,
	}
	sessionCacheMu.Lock()
	sessionCache[cacheKey] = newEntry
	sessionCacheMu.Unlock()

	cp := *data
	return &cp, nil
}

// findSessionByContent reads JSONL content for each candidate until it finds a
// session whose LastActivity is within the process uptime window.
// Returns the chosen *SessionData and the file path used.
func findSessionByContent(candidates []sessionFileCandidate, uptimeSeconds int64, cwd string) (*SessionData, string, error) {
	var bestByContent *SessionData
	var bestByContentPath string
	for _, c := range candidates {
		data, err := ParseSessionFile(c.path)
		if err != nil {
			continue
		}
		age := time.Since(data.LastActivity)
		if age < time.Duration(uptimeSeconds+10)*time.Second {
			return data, c.path, nil
		}
		// Keep the first (most-recently modified) as fallback.
		if bestByContent == nil {
			bestByContent = data
			bestByContentPath = c.path
		}
	}
	if bestByContent != nil {
		return bestByContent, bestByContentPath, nil
	}
	return nil, "", fmt.Errorf("no active session for %s", cwd)
}

// ParseSessionFile parses a single JSONL session file and returns its SessionData.
func ParseSessionFile(path string) (*SessionData, error) {
	content, err := TailRead(path)
	if err != nil {
		return nil, err
	}

	data := &SessionData{
		ToolCounts:   make(map[string]int),
		Entrypoint:   sdk.EntrypointUnknown,
		LastActivity: time.Now().Add(-24 * time.Hour), // default: old
	}

	var recentToolNames []string

	scanner := bufio.NewScanner(bytes.NewReader([]byte(content)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry jsonlMessage
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		// Accept both the legacy "message" envelope and the current direct "assistant" type.
		if entry.Type != "assistant" && entry.Type != "message" {
			continue
		}
		var msg msgContent
		if err := json.Unmarshal(entry.Message, &msg); err != nil {
			continue
		}

		if msg.Role == "assistant" {
			data.ConversationTurns++
			if msg.Model != "" {
				data.Model = msg.Model
			}
			if msg.Usage != nil {
				data.TokenUsage.InputTokens += msg.Usage.InputTokens
				data.TokenUsage.OutputTokens += msg.Usage.OutputTokens
				data.TokenUsage.CacheCreationTokens += msg.Usage.CacheCreationTokens
				data.TokenUsage.CacheReadTokens += msg.Usage.CacheReadTokens
			}
			if ts, parseErr := time.Parse(time.RFC3339Nano, entry.Timestamp); parseErr == nil {
				if ts.After(data.LastActivity) {
					data.LastActivity = ts
				}
			}
			// If parse fails, LastActivity stays at its default (-24h old) — agent looks idle, which is correct.

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
		}
	}
	if err := scanner.Err(); err != nil {
		slog.Warn("parser: session scan error — partial data returned", "err", err)
	}

	if len(recentToolNames) > 5 {
		data.LastTools = recentToolNames[len(recentToolNames)-5:]
	} else {
		data.LastTools = recentToolNames
	}

	// Convergence detection: last 5 tools identical
	if len(recentToolNames) >= 5 {
		last5 := recentToolNames[len(recentToolNames)-5:]
		allSame := true
		for _, t := range last5[1:] {
			if t != last5[0] {
				allSame = false
				break
			}
		}
		if allSame {
			data.ConvergenceAlert = true
			data.ConvergenceToolName = last5[0]
		}
	}

	return data, nil
}

func loadSessionMeta(sessionID string) *sdk.SessionMeta {
	path := filepath.Join(sessionMetaDir(), sessionID+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var meta sdk.SessionMeta
	if err := json.Unmarshal(b, &meta); err != nil {
		return nil
	}
	return &meta
}
