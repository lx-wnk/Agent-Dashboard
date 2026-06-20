package parser

import (
	"bufio"
	"encoding/json"
	"os"
	"sync"
	"time"
)

// SubagentParse holds the aggregated result of parsing a subagent JSONL file.
type SubagentParse struct {
	TokensUsed      int
	DurationSeconds int
	LatestOutput    string
	CurrentAction   string
	LastActivity    time.Time
}

// subagentLine is the minimal envelope for a single JSONL line.
type subagentLine struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Message   json.RawMessage `json:"message"`
}

type subagentMessage struct {
	Role    string            `json:"role"`
	Content []json.RawMessage `json:"content"`
	Usage   *subagentUsage    `json:"usage"`
}

type subagentUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

type subagentContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
	Name string `json:"name"`
}

// ParseSubagentFile reads path from disk and returns aggregated parse results.
func ParseSubagentFile(path string) (SubagentParse, error) {
	f, err := os.Open(path)
	if err != nil {
		return SubagentParse{}, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	buf := make([]byte, 64*1024)
	sc.Buffer(buf, 4*1024*1024)

	var (
		result  SubagentParse
		firstTS time.Time
		lastTS  time.Time
	)

	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}

		var envelope subagentLine
		if err := json.Unmarshal(line, &envelope); err != nil {
			continue
		}

		if envelope.Timestamp != "" {
			ts, err := time.Parse(time.RFC3339Nano, envelope.Timestamp)
			if err == nil {
				if firstTS.IsZero() {
					firstTS = ts
				}
				lastTS = ts
			}
		}

		if envelope.Type != "assistant" || len(envelope.Message) == 0 {
			continue
		}

		var msg subagentMessage
		if err := json.Unmarshal(envelope.Message, &msg); err != nil {
			continue
		}

		if msg.Usage != nil {
			result.TokensUsed += msg.Usage.InputTokens +
				msg.Usage.OutputTokens +
				msg.Usage.CacheCreationInputTokens +
				msg.Usage.CacheReadInputTokens
		}

		for _, rawBlock := range msg.Content {
			var block subagentContentBlock
			if err := json.Unmarshal(rawBlock, &block); err != nil {
				continue
			}
			switch block.Type {
			case "text":
				result.LatestOutput = block.Text
				if len(result.LatestOutput) > 1000 {
					result.LatestOutput = result.LatestOutput[:1000]
				}
			case "tool_use":
				result.CurrentAction = block.Name
			}
		}
	}

	if err := sc.Err(); err != nil {
		return SubagentParse{}, err
	}

	if !firstTS.IsZero() && !lastTS.IsZero() {
		result.DurationSeconds = int(lastTS.Sub(firstTS).Seconds())
	}
	result.LastActivity = lastTS

	return result, nil
}

type subagentCacheEntry struct {
	mtime  time.Time
	parsed SubagentParse
}

var (
	subagentCacheMu sync.RWMutex
	subagentCache   = map[string]subagentCacheEntry{}
)

// PruneSubagentCache removes cache entries whose path is not in livePaths.
// Call once per directory walk after collecting the current set of subagent
// file paths to prevent unbounded growth.
func PruneSubagentCache(livePaths map[string]bool) {
	subagentCacheMu.Lock()
	defer subagentCacheMu.Unlock()
	for path := range subagentCache {
		if !livePaths[path] {
			delete(subagentCache, path)
		}
	}
}

// ParseSubagentFileCached returns a cached parse result when the file mtime is
// unchanged, otherwise re-parses and updates the cache.
func ParseSubagentFileCached(path string) (SubagentParse, error) {
	info, err := os.Stat(path)
	if err != nil {
		return SubagentParse{}, err
	}
	mtime := info.ModTime()

	subagentCacheMu.RLock()
	entry, ok := subagentCache[path]
	subagentCacheMu.RUnlock()

	if ok && entry.mtime.Equal(mtime) {
		return entry.parsed, nil
	}

	parsed, err := ParseSubagentFile(path)
	if err != nil {
		return SubagentParse{}, err
	}

	subagentCacheMu.Lock()
	subagentCache[path] = subagentCacheEntry{mtime: mtime, parsed: parsed}
	subagentCacheMu.Unlock()

	return parsed, nil
}
