package parser

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/scanner"
)

const (
	maxSessions  = 100
	maxReadBytes = 10 * 1024 * 1024 // 10 MB cap for ParseFullSession
)

// SessionInfo is the session-list entry returned by GET /api/sessions.
type SessionInfo struct {
	SessionID         string  `json:"sessionId"`
	ProjectPath       string  `json:"projectPath"`
	ProjectName       string  `json:"projectName"`
	LastModified      string  `json:"lastModified"` // ISO 8601
	Model             *string `json:"model"`
	FirstPrompt       *string `json:"firstPrompt"`
	LastResponse      *string `json:"lastResponse"`
	TotalInputTokens  int     `json:"totalInputTokens"`
	TotalOutputTokens int     `json:"totalOutputTokens"`
	CostEstimate      float64 `json:"costEstimate"`
	IsRunning         bool    `json:"isRunning"`
}

// OutputMessage is a single displayable message from a session transcript.
type OutputMessage struct {
	Role         string  `json:"role"` // assistant | tool_call | tool_result | human | task | subagent
	Content      string  `json:"content"`
	Timestamp    *string `json:"timestamp,omitempty"`
	ToolName     *string `json:"toolName,omitempty"`
	FilePath     *string `json:"filePath,omitempty"`
	TaskStatus   *string `json:"taskStatus,omitempty"`
	TaskID       *string `json:"taskId,omitempty"`
	SubagentType *string `json:"subagentType,omitempty"`
	Queued       bool    `json:"queued,omitempty"`
}

type jsonlFileEntry struct {
	sessionID          string
	filePath           string
	projectDirEncoded  string
	mtime              time.Time
}

// GetSessions scans ~/.claude/projects/ and returns the 100 most recently
// modified sessions enriched with token counts, model, and first/last content.
func GetSessions(ctx context.Context) ([]SessionInfo, error) {
	projectsDir := claudeProjectsDir()

	projectDirs, err := os.ReadDir(projectsDir)
	if err != nil {
		return []SessionInfo{}, nil
	}

	// Collect all .jsonl files
	var allFiles []jsonlFileEntry
	for _, dirEntry := range projectDirs {
		if !dirEntry.IsDir() {
			continue
		}
		dirPath := filepath.Join(projectsDir, dirEntry.Name())
		files, err := os.ReadDir(dirPath)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			sessionID := strings.TrimSuffix(f.Name(), ".jsonl")
			if !uuidRE.MatchString(sessionID) {
				continue
			}
			fullPath := filepath.Join(dirPath, f.Name())
			info, err := f.Info()
			if err != nil {
				continue
			}
			allFiles = append(allFiles, jsonlFileEntry{
				sessionID:         sessionID,
				filePath:          fullPath,
				projectDirEncoded: dirEntry.Name(),
				mtime:             info.ModTime(),
			})
		}
	}

	// Sort newest first, cap at maxSessions
	sort.Slice(allFiles, func(i, j int) bool {
		return allFiles[i].mtime.After(allFiles[j].mtime)
	})
	if len(allFiles) > maxSessions {
		allFiles = allFiles[:maxSessions]
	}

	// Determine running CWD set
	runningEncoded := make(map[string]bool)
	if procs, err := scanner.ScanProcesses(ctx); err == nil {
		for _, p := range procs {
			runningEncoded[EncodePath(p.CWD)] = true
		}
	}

	sessions := make([]SessionInfo, 0, len(allFiles))
	for _, entry := range allFiles {
		si := buildSessionInfo(entry, runningEncoded)
		sessions = append(sessions, si)
	}
	return sessions, nil
}

func buildSessionInfo(entry jsonlFileEntry, runningEncoded map[string]bool) SessionInfo {
	meta := loadSessionMeta(entry.sessionID)

	var model *string
	var projectPath string
	var lastResponse *string

	// Head-read for model + cwd; tail-read for last assistant response
	if headRaw, err := headRead(entry.filePath); err == nil {
		if m, cwd := extractHeadInfoRaw(headRaw); m != "" {
			model = &m
			if cwd != "" {
				projectPath = cwd
			}
		}
	}
	if projectPath == "" {
		projectPath = entry.projectDirEncoded
	}

	if tailRaw, err := TailRead(entry.filePath); err == nil {
		if resp := extractLastAssistantText(tailRaw); resp != "" {
			lastResponse = &resp
		}
	}

	var inputTokens, outputTokens int
	var firstPrompt *string
	if meta != nil {
		inputTokens = meta.InputTokens
		outputTokens = meta.OutputTokens
		if meta.FirstPrompt != "" {
			firstPrompt = &meta.FirstPrompt
		}
	}

	var costEstimate float64
	if model != nil {
		costEstimate = EstimateCost(sdk.TokenUsage{
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
		}, *model)
	}

	return SessionInfo{
		SessionID:         entry.sessionID,
		ProjectPath:       projectPath,
		ProjectName:       filepath.Base(projectPath),
		LastModified:      entry.mtime.UTC().Format(time.RFC3339),
		Model:             model,
		FirstPrompt:       firstPrompt,
		LastResponse:      lastResponse,
		TotalInputTokens:  inputTokens,
		TotalOutputTokens: outputTokens,
		CostEstimate:      costEstimate,
		IsRunning:         runningEncoded[entry.projectDirEncoded],
	}
}

// headRead reads up to 8KB from the start of a file.
func headRead(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, 8192)
	n, err := io.ReadAtLeast(f, buf, 1)
	if err != nil && n == 0 {
		return "", err
	}
	return string(buf[:n]), nil
}

// headEntry is the minimal JSONL structure for head-reads (model + cwd).
type headEntry struct {
	Type    string `json:"type"`
	CWD     string `json:"cwd"`
	Message struct {
		Model string `json:"model"`
	} `json:"message"`
}

func extractHeadInfoRaw(raw string) (model, cwd string) {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e headEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		if model == "" && e.Message.Model != "" {
			model = e.Message.Model
		}
		if cwd == "" && e.CWD != "" {
			cwd = e.CWD
		}
		if model != "" && cwd != "" {
			break
		}
	}
	return
}

// extractLastAssistantText returns the last non-empty assistant text block
// from a raw JSONL string, truncated to 1000 chars.
func extractLastAssistantText(raw string) string {
	type contentBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type msgBody struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	type entry struct {
		Type    string          `json:"type"`
		Message json.RawMessage `json:"message"`
	}

	var last string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e entry
		if err := json.Unmarshal([]byte(line), &e); err != nil || e.Type != "message" {
			continue
		}
		var msg msgBody
		if err := json.Unmarshal(e.Message, &msg); err != nil || msg.Role != "assistant" {
			continue
		}
		var blocks []contentBlock
		if err := json.Unmarshal(msg.Content, &blocks); err != nil {
			continue
		}
		var sb strings.Builder
		for _, b := range blocks {
			if b.Type == "text" {
				sb.WriteString(b.Text)
			}
		}
		if text := strings.TrimSpace(sb.String()); text != "" {
			last = text
		}
	}
	if len(last) > 1000 {
		last = last[:1000]
	}
	return last
}

// ParseFullSession reads an entire session JSONL (capped at 10 MB) and returns
// all messages in display order. If lastOnly is true, only the last assistant
// message is returned.
func ParseFullSession(sessionID string, lastOnly bool) ([]OutputMessage, error) {
	if !uuidRE.MatchString(sessionID) {
		return nil, nil
	}

	projectsDir := claudeProjectsDir()
	dirs, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil, nil
	}

	var sessionPath string
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		candidate := filepath.Join(projectsDir, d.Name(), sessionID+".jsonl")
		if _, err := os.Stat(candidate); err == nil {
			sessionPath = candidate
			break
		}
	}
	if sessionPath == "" {
		return nil, nil
	}

	raw, err := readSessionRaw(sessionPath, lastOnly)
	if err != nil || raw == "" {
		return nil, err
	}
	return parseOutputMessages(raw, lastOnly), nil
}

func readSessionRaw(path string, lastOnly bool) (string, error) {
	if lastOnly {
		return TailRead(path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.Size() <= maxReadBytes {
		b, err := os.ReadFile(path)
		return string(b), err
	}
	// Large file: read last maxReadBytes
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Seek(-maxReadBytes, io.SeekEnd); err != nil {
		return "", err
	}
	buf := make([]byte, maxReadBytes)
	n, err := io.ReadFull(f, buf)
	if err != nil && n == 0 {
		return "", err
	}
	return string(buf[:n]), nil
}

// rawEntry is the top-level structure of a JSONL session log line.
type rawEntry struct {
	Type       string          `json:"type"`
	Timestamp  string          `json:"timestamp"`
	Message    json.RawMessage `json:"message"`
	Result     json.RawMessage `json:"result"`
	Attachment json.RawMessage `json:"attachment"`
	CWD        string          `json:"cwd"`
}

type rawMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	Model   string          `json:"model"`
}

type rawBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Name      string          `json:"name"`
	ID        string          `json:"id"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	Input     json.RawMessage `json:"input"`
}

type taskCreateInput struct {
	Subject     string `json:"subject"`
	Description string `json:"description"`
}

type taskUpdateInput struct {
	TaskID string `json:"taskId"`
	Status string `json:"status"`
}

type agentInput struct {
	Description  string `json:"description"`
	SubagentType string `json:"subagent_type"`
}

type toolInput struct {
	FilePath string `json:"file_path"`
	Path     string `json:"path"`
}

type queuedAttachment struct {
	Type   string          `json:"type"`
	Prompt json.RawMessage `json:"prompt"`
}

func parseOutputMessages(raw string, lastOnly bool) []OutputMessage {
	var messages []OutputMessage
	taskCreateIndices := make(map[string]int)    // tool_use_id → index in messages
	taskSubjects := make(map[string]string)      // tool_use_id → subject

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e rawEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		ts := strPtr(e.Timestamp)

		// Queued commands
		if e.Type == "attachment" {
			var att queuedAttachment
			if err := json.Unmarshal(e.Attachment, &att); err == nil && att.Type == "queued_command" {
				for _, text := range extractTextFromPrompt(att.Prompt) {
					msg := OutputMessage{Role: "human", Content: text, Timestamp: ts, Queued: true}
					messages = append(messages, msg)
				}
			}
			continue
		}

		// Standalone tool results (older format)
		if e.Type == "result" && len(e.Result) > 0 {
			var resultStr string
			if err := json.Unmarshal(e.Result, &resultStr); err != nil {
				resultStr = string(e.Result)
			}
			if len(resultStr) > 1000 {
				resultStr = resultStr[:1000]
			}
			messages = append(messages, OutputMessage{Role: "tool_result", Content: resultStr, Timestamp: ts})
			continue
		}

		if len(e.Message) == 0 {
			continue
		}
		var msg rawMessage
		if err := json.Unmarshal(e.Message, &msg); err != nil {
			continue
		}

		// User messages
		if e.Type == "user" && msg.Role == "user" {
			for _, text := range extractTextFromContent(msg.Content) {
				messages = append(messages, OutputMessage{Role: "human", Content: text, Timestamp: ts})
			}
			// Tool results inside user message
			var blocks []rawBlock
			if err := json.Unmarshal(msg.Content, &blocks); err == nil {
				for _, b := range blocks {
					if b.Type != "tool_result" || len(b.Content) == 0 {
						continue
					}
					var content string
					if err := json.Unmarshal(b.Content, &content); err != nil {
						content = string(b.Content)
					}
					if len(content) > 1000 {
						content = content[:1000]
					}
					// Resolve TaskCreate → real ID
					if b.ToolUseID != "" {
						if idx, ok := taskCreateIndices[b.ToolUseID]; ok {
							realID := strings.TrimSpace(content)
							if realID != "" {
								messages[idx].TaskID = &realID
								if subj, ok2 := taskSubjects[b.ToolUseID]; ok2 {
									taskSubjects[realID] = subj
								}
							}
						}
					}
					messages = append(messages, OutputMessage{Role: "tool_result", Content: content, Timestamp: ts})
				}
			}
			continue
		}

		// Assistant messages
		if e.Type != "assistant" {
			continue
		}
		var blocks []rawBlock
		if err := json.Unmarshal(msg.Content, &blocks); err != nil {
			continue
		}
		for _, b := range blocks {
			switch b.Type {
			case "text":
				text := strings.TrimSpace(b.Text)
				if text == "" {
					continue
				}
				messages = append(messages, OutputMessage{Role: "assistant", Content: text, Timestamp: ts})

			case "tool_use":
				if b.Name == "" {
					continue
				}
				switch b.Name {
				case "TaskCreate":
					var inp taskCreateInput
					_ = json.Unmarshal(b.Input, &inp)
					subject := inp.Subject
					if subject == "" {
						subject = inp.Description
					}
					if subject == "" {
						subject = "Task"
					}
					taskCreateIndices[b.ID] = len(messages)
					taskSubjects[b.ID] = subject
					taskStatus := "pending"
					taskID := b.ID
					messages = append(messages, OutputMessage{
						Role:       "task",
						Content:    subject,
						Timestamp:  ts,
						TaskStatus: &taskStatus,
						TaskID:     &taskID,
					})

				case "TaskUpdate":
					var inp taskUpdateInput
					_ = json.Unmarshal(b.Input, &inp)
					subject := taskSubjects[inp.TaskID]
					if subject == "" {
						subject = "Task"
					}
					status := inp.Status
					if status == "" {
						status = "in_progress"
					}
					realID := inp.TaskID
					messages = append(messages, OutputMessage{
						Role:       "task",
						Content:    subject,
						Timestamp:  ts,
						TaskStatus: &status,
						TaskID:     &realID,
					})

				case "Agent":
					var inp agentInput
					_ = json.Unmarshal(b.Input, &inp)
					content := inp.Description
					if content == "" {
						content = "Sub-agent"
					}
					subType := inp.SubagentType
					if subType == "" {
						subType = "general"
					}
					messages = append(messages, OutputMessage{
						Role:         "subagent",
						Content:      content,
						Timestamp:    ts,
						SubagentType: &subType,
					})

				default:
					var inp toolInput
					_ = json.Unmarshal(b.Input, &inp)
					var fp *string
					if inp.FilePath != "" {
						fp = &inp.FilePath
					} else if inp.Path != "" {
						fp = &inp.Path
					}
					toolName := b.Name
					messages = append(messages, OutputMessage{
						Role:      "tool_call",
						Content:   b.Name,
						Timestamp: ts,
						ToolName:  &toolName,
						FilePath:  fp,
					})
				}
			}
		}
	}

	if lastOnly {
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == "assistant" {
				return messages[i : i+1]
			}
		}
		return nil
	}
	return messages
}

// extractTextFromContent extracts plain text from a content field (string or block array).
func extractTextFromContent(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	// Try string first
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if t := strings.TrimSpace(s); t != "" {
			return []string{t}
		}
		return nil
	}
	// Try block array
	var blocks []rawBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil
	}
	var out []string
	for _, b := range blocks {
		if b.Type == "text" {
			if t := strings.TrimSpace(b.Text); t != "" {
				out = append(out, t)
			}
		}
	}
	return out
}

// extractTextFromPrompt extracts text parts from a queued_command prompt.
func extractTextFromPrompt(raw json.RawMessage) []string {
	return extractTextFromContent(raw)
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
