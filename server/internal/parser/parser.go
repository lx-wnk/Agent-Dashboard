package parser

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
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
func AllClaudeConfigDirs() []string {
	return allClaudeConfigDirs()
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
	Entrypoint          string // "cli" | "desktop" | "unknown"
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
	ErrorState          string
	Meta                *sdk.SessionMeta
}

// FindSessionForProject locates the most recently active JSONL session for cwd.
// claudeConfigDir, if non-empty, overrides the default ~/.claude config directory
// (use the value of CLAUDE_CONFIG_DIR from the process environment).
func FindSessionForProject(cwd string, uptimeSeconds int64, claudeConfigDir string) (*SessionData, error) {
	baseDir := claudeProjectsDir()
	if claudeConfigDir != "" {
		baseDir = filepath.Join(claudeConfigDir, "projects")
	}
	encoded := EncodePath(cwd)
	projectDir := filepath.Join(baseDir, encoded)

	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return nil, fmt.Errorf("readdir %s: %w", projectDir, err)
	}

	type candidate struct {
		path  string
		mtime time.Time
	}
	var candidates []candidate
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
		candidates = append(candidates, candidate{
			path:  filepath.Join(projectDir, name),
			mtime: info.ModTime(),
		})
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no session files in %s", projectDir)
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].mtime.After(candidates[j].mtime)
	})

	var bestByContent *SessionData
	var bestByContentPath string
	for _, c := range candidates {
		data, err := parseSessionFile(c.path)
		if err != nil {
			continue
		}
		age := time.Since(data.LastActivity)
		if age < time.Duration(uptimeSeconds+10)*time.Second {
			data.SessionID = strings.TrimSuffix(filepath.Base(c.path), ".jsonl")
			data.ProjectPath = cwd
			data.Meta = loadSessionMeta(data.SessionID)
			return data, nil
		}
		// Keep the first (most-recently modified) as fallback in case no entry matches.
		if bestByContent == nil {
			bestByContent = data
			bestByContentPath = c.path
		}
	}
	// Fallback: the process is alive but its session was cleared or very old.
	// Return the most-recently modified session so the card still shows the agent.
	if bestByContent != nil {
		bestByContent.SessionID = strings.TrimSuffix(filepath.Base(bestByContentPath), ".jsonl")
		bestByContent.ProjectPath = cwd
		bestByContent.Meta = loadSessionMeta(bestByContent.SessionID)
		return bestByContent, nil
	}
	return nil, fmt.Errorf("no active session for %s", cwd)
}

// ParseSessionFile parses a single JSONL session file and returns its SessionData.
// Exported for use in tests and external consumers.
func ParseSessionFile(path string) (*SessionData, error) {
	return parseSessionFile(path)
}

func parseSessionFile(path string) (*SessionData, error) {
	content, err := TailRead(path)
	if err != nil {
		return nil, err
	}

	data := &SessionData{
		ToolCounts:   make(map[string]int),
		Entrypoint:   "unknown",
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
				for _, b := range blocks {
					switch b.Type {
					case "tool_use":
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
							data.LastOutput = scrubSecrets(b.Text)
							// Detect error states (check original text before scrubbing)
							switch {
							case quotaRE.MatchString(b.Text):
								data.ErrorState = "quota_exhausted"
							case rateRE.MatchString(b.Text):
								data.ErrorState = "rate_limited"
							case authRE.MatchString(b.Text):
								data.ErrorState = "auth_failed"
							}
						}
					}
				}
			}
		}
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
