// Package analytics implements workflow pattern discovery via JSONL ngram analysis.
package analytics

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	ngramSize   = 3
	maxPatterns = 20
	maxFileSize = 10 * 1024 * 1024 // 10 MB per file cap
)

var toolNameRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)

// allClaudeConfigDirs returns all candidate Claude config directories.
// Mirrors the unexported parser.allClaudeConfigDirs logic.
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
	// 1. Explicit dashboard config list.
	if val := os.Getenv("DASHBOARD_CLAUDE_CONFIG_DIRS"); val != "" {
		for _, p := range strings.Split(val, ",") {
			add(p)
		}
	}
	// 2. Server process CLAUDE_CONFIG_DIR.
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		add(dir)
	}
	// 3. Default ~/.claude.
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

// patternEntry holds a discovered ngram and its occurrence count.
type patternEntry struct {
	tools     string
	frequency int
}

// ExtractNgrams computes 3-grams from a slice of tool names.
// Returns a map from gram string → occurrence count.
func ExtractNgrams(toolSequence []string) map[string]int {
	counts := make(map[string]int)
	if len(toolSequence) < ngramSize {
		return counts
	}
	for i := 0; i <= len(toolSequence)-ngramSize; i++ {
		gram := strings.Join(toolSequence[i:i+ngramSize], " → ")
		counts[gram]++
	}
	return counts
}

// DiscoverPatterns iterates all JSONL session files, extracts tool_use events,
// computes 3-grams, and upserts the top-20 patterns into workflow_patterns.
func DiscoverPatterns(db *sql.DB) error {
	globalCounts := make(map[string]int)

	for _, configDir := range allClaudeConfigDirs() {
		projectsDir := filepath.Join(configDir, "projects")
		projectDirs, err := os.ReadDir(projectsDir)
		if err != nil {
			continue
		}
		for _, pDir := range projectDirs {
			if !pDir.IsDir() {
				continue
			}
			dirPath := filepath.Join(projectsDir, pDir.Name())
			files, err := os.ReadDir(dirPath)
			if err != nil {
				continue
			}
			for _, f := range files {
				if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
					continue
				}
				filePath := filepath.Join(dirPath, f.Name())
				tools, err := extractToolsFromJSONL(filePath)
				if err != nil {
					slog.Debug("analytics: skip file", "path", filePath, "err", err)
					continue
				}
				for gram, count := range ExtractNgrams(tools) {
					globalCounts[gram] += count
				}
			}
		}
	}

	if len(globalCounts) == 0 {
		return nil
	}

	// Sort by frequency descending, cap at top 20.
	entries := make([]patternEntry, 0, len(globalCounts))
	for gram, count := range globalCounts {
		entries = append(entries, patternEntry{tools: gram, frequency: count})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].frequency != entries[j].frequency {
			return entries[i].frequency > entries[j].frequency
		}
		return entries[i].tools < entries[j].tools
	})
	if len(entries) > maxPatterns {
		entries = entries[:maxPatterns]
	}

	return upsertPatterns(db, entries)
}

// upsertPatterns writes the discovered patterns into the workflow_patterns table.
func upsertPatterns(db *sql.DB, entries []patternEntry) error {
	now := time.Now().UTC().Format(time.RFC3339)
	const stmt = `
INSERT INTO workflow_patterns (tools, frequency, last_seen_at)
VALUES (?, ?, ?)
ON CONFLICT(tools) DO UPDATE SET
    frequency = excluded.frequency,
    last_seen_at = excluded.last_seen_at
`
	for _, e := range entries {
		if _, err := db.Exec(stmt, e.tools, e.frequency, now); err != nil {
			return fmt.Errorf("analytics: upsert pattern %q: %w", e.tools, err)
		}
	}
	return nil
}

// extractToolsFromJSONL reads a JSONL file (capped at maxFileSize) and returns
// all tool names in the order they appear in assistant messages.
func extractToolsFromJSONL(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	size := info.Size()
	if size > maxFileSize {
		size = maxFileSize
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// If file is large, tail-read the last maxFileSize bytes.
	if info.Size() > maxFileSize {
		if _, err := f.Seek(-maxFileSize, io.SeekEnd); err != nil {
			return nil, err
		}
	}

	buf := make([]byte, size)
	n, err := io.ReadAtLeast(f, buf, 1)
	if err != nil && n == 0 {
		return nil, err
	}
	raw := string(buf[:n])

	return parseToolsFromRaw(raw), nil
}

// jsonlEntry is the minimal parse target for JSONL lines.
type jsonlEntry struct {
	Type    string          `json:"type"`
	Message json.RawMessage `json:"message"`
}

type jsonlMessage struct {
	Role    string            `json:"role"`
	Content []json.RawMessage `json:"content"`
}

type jsonlContentBlock struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

// parseToolsFromRaw extracts tool names from raw JSONL content.
func parseToolsFromRaw(raw string) []string {
	var tools []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry jsonlEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Type != "assistant" && entry.Type != "message" {
			continue
		}
		if len(entry.Message) == 0 {
			continue
		}
		var msg jsonlMessage
		if err := json.Unmarshal(entry.Message, &msg); err != nil {
			continue
		}
		if msg.Role != "assistant" {
			continue
		}
		for _, rawBlock := range msg.Content {
			var block jsonlContentBlock
			if err := json.Unmarshal(rawBlock, &block); err != nil {
				continue
			}
			if block.Type == "tool_use" && toolNameRE.MatchString(block.Name) {
				tools = append(tools, block.Name)
			}
		}
	}
	return tools
}
