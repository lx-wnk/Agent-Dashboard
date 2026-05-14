package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
)

var jsonBlockRE = regexp.MustCompile("(?s)```json\\b([\\s\\S]*?)```")

func ResolvedProjectDir(cwd string) (string, error) {
	resolved, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		resolved = cwd
	}
	return filepath.Join(parser.ClaudeProjectsDir(), parser.EncodePath(resolved)), nil
}

func FindNewestSessionID(cwd, afterISO string) (string, error) {
	projectDir, err := ResolvedProjectDir(cwd)
	if err != nil {
		return "", fmt.Errorf("FindNewestSessionID.resolveDir: %w", err)
	}
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return "", nil
	}
	type candidate struct {
		sessionID string
		mtime     int64
	}
	var candidates []candidate
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		sessionID := strings.TrimSuffix(e.Name(), ".jsonl")
		candidates = append(candidates, candidate{sessionID: sessionID, mtime: info.ModTime().UnixMilli()})
	}
	if len(candidates) == 0 {
		return "", nil
	}
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.mtime > best.mtime {
			best = c
		}
	}
	return best.sessionID, nil
}

type StageOutputRead struct {
	Output  map[string]any
	RawText string
}

func ExtractJsonBlock(text string) map[string]any {
	matches := jsonBlockRE.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	last := matches[len(matches)-1][1]
	var result map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(last)), &result); err != nil {
		return nil
	}
	return result
}

type JsonlEntry struct {
	Type    string `json:"type"`
	Message *struct {
		Role    string          `json:"role"`
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
		Usage   *struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

func lastAssistantText(entries []JsonlEntry) string {
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Type != "assistant" || e.Message == nil {
			continue
		}
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(e.Message.Content, &parts); err != nil {
			continue
		}
		var texts []string
		for _, p := range parts {
			if p.Type == "text" && p.Text != "" {
				texts = append(texts, p.Text)
			}
		}
		if len(texts) > 0 {
			return strings.Join(texts, "\n")
		}
	}
	return ""
}

func ReadLastStageJsonOutput(cwd, sessionID string) (StageOutputRead, error) {
	projectDir, err := ResolvedProjectDir(cwd)
	if err != nil {
		return StageOutputRead{}, fmt.Errorf("ReadLastStageJsonOutput.resolveDir: %w", err)
	}
	filePath := filepath.Join(projectDir, sessionID+".jsonl")
	raw, err := parser.TailRead(filePath)
	if err != nil {
		return StageOutputRead{}, nil
	}
	entries := parseJsonlLines(raw)
	text := lastAssistantText(entries)
	if text == "" {
		return StageOutputRead{}, nil
	}
	return StageOutputRead{Output: ExtractJsonBlock(text), RawText: text}, nil
}

type SessionTokenSummary struct {
	InputTokens         int
	OutputTokens        int
	CacheCreationTokens int
	CacheReadTokens     int
	Model               string
}

func ReadSessionTokenSummary(cwd, sessionID string) (SessionTokenSummary, error) {
	projectDir, err := ResolvedProjectDir(cwd)
	if err != nil {
		return SessionTokenSummary{}, fmt.Errorf("ReadSessionTokenSummary.resolveDir: %w", err)
	}
	filePath := filepath.Join(projectDir, sessionID+".jsonl")
	raw, err := parser.TailRead(filePath)
	if err != nil {
		return SessionTokenSummary{}, nil
	}
	entries := parseJsonlLines(raw)
	var summary SessionTokenSummary
	for _, e := range entries {
		if e.Type != "assistant" || e.Message == nil {
			continue
		}
		if e.Message.Model != "" && summary.Model == "" {
			summary.Model = e.Message.Model
		}
		if u := e.Message.Usage; u != nil {
			summary.InputTokens += u.InputTokens
			summary.OutputTokens += u.OutputTokens
			summary.CacheCreationTokens += u.CacheCreationInputTokens
			summary.CacheReadTokens += u.CacheReadInputTokens
		}
	}
	return summary, nil
}

func parseJsonlLines(raw string) []JsonlEntry {
	var entries []JsonlEntry
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e JsonlEntry
		if err := json.Unmarshal([]byte(line), &e); err == nil {
			entries = append(entries, e)
		}
	}
	return entries
}
