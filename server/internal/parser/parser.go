package parser

import (
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

// claudeProjectsDir returns ~/.claude/projects
func claudeProjectsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects")
}

// sessionMetaDir returns ~/.claude/usage-data/session-meta
func sessionMetaDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "usage-data", "session-meta")
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
	Type    string          `json:"type"`
	Message json.RawMessage `json:"message"`
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
func FindSessionForProject(cwd string, uptimeSeconds int64) (*SessionData, error) {
	encoded := EncodePath(cwd)
	projectDir := filepath.Join(claudeProjectsDir(), encoded)

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
	}
	return nil, fmt.Errorf("no active session for %s", cwd)
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

	// TODO(phase1): Tasks field not yet populated — task extraction from TodoWrite/TodoRead tool inputs is deferred.
	var recentToolNames []string

	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry jsonlMessage
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Type != "message" {
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
			data.LastActivity = time.Now()

			var blocks []toolUseBlock
			if err := json.Unmarshal(msg.Content, &blocks); err == nil {
				for _, b := range blocks {
					switch b.Type {
					case "tool_use":
						data.ToolCounts[b.Name]++
						recentToolNames = append(recentToolNames, b.Name)
						data.CurrentAction = b.Name
					case "text":
						if b.Text != "" {
							data.LastOutput = b.Text
							// Detect error states
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
