package analytics

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
)

// sessionFileRE matches a session JSONL filename (UUID + .jsonl) — used to
// filter out hidden / temp files when walking project directories.
var sessionFileRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.jsonl$`)

// SessionFile is a discovered JSONL file with its session ID and mtime.
type SessionFile struct {
	SessionID string
	Path      string
	ModTime   time.Time
}

// DiscoverSessions walks every configured Claude config dir and returns
// session JSONL files filtered by mtime and the optional Sessions
// allow-list. Results are sorted by mtime descending and truncated to
// MaxSessions (or DefaultMaxSessions if zero).
//
// The dirs argument is provided for tests; production callers pass nil to
// fall back to parser.AllClaudeConfigDirs().
func DiscoverSessions(opts ScanOpts, dirs []string) []SessionFile {
	if dirs == nil {
		dirs = parser.AllClaudeConfigDirs()
	}
	allow := map[string]bool{}
	for _, id := range opts.Sessions {
		if id != "" {
			allow[id] = true
		}
	}

	var out []SessionFile
	for _, configDir := range dirs {
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
				name := f.Name()
				if f.IsDir() || !sessionFileRE.MatchString(name) {
					continue
				}
				info, err := f.Info()
				if err != nil {
					continue
				}
				mtime := info.ModTime()
				if !opts.From.IsZero() && mtime.Before(opts.From) {
					continue
				}
				if !opts.To.IsZero() && mtime.After(opts.To) {
					continue
				}
				sessID := strings.TrimSuffix(name, ".jsonl")
				if len(allow) > 0 && !allow[sessID] {
					continue
				}
				out = append(out, SessionFile{
					SessionID: sessID,
					Path:      filepath.Join(dirPath, name),
					ModTime:   mtime,
				})
			}
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ModTime.After(out[j].ModTime) })

	limit := opts.MaxSessions
	if limit <= 0 {
		limit = DefaultMaxSessions
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// scanSessionsForTools loads ToolCalls across the sessions selected by
// DiscoverSessions. Files larger than maxFileSize are skipped with a
// slog warning. Returns a map keyed by session ID where each value is
// the in-order tool_use timeline for that session.
func scanSessionsForTools(ctx context.Context, opts ScanOpts, dirs []string) (map[string][]ToolCall, error) {
	files := DiscoverSessions(opts, dirs)
	result := make(map[string][]ToolCall, len(files))
	for _, sf := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		calls, err := readToolCallsFromFile(sf.Path, sf.SessionID, opts.From, opts.To)
		if err != nil {
			slog.Debug("analytics: skip session file", "path", sf.Path, "err", err)
			continue
		}
		if len(calls) > 0 {
			result[sf.SessionID] = calls
		}
	}
	return result, nil
}

// readToolCallsFromFile streams a single JSONL file and emits one
// ToolCall per assistant tool_use block. Honors the per-file size cap
// from ngrams.go and the time bounds from ScanOpts.
func readToolCallsFromFile(path, sessionID string, from, to time.Time) ([]ToolCall, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxFileSize {
		slog.Warn("analytics: session file exceeds cap, tailing", "path", path, "size", info.Size())
	}

	rc, err := parser.OpenJSONLReader(path, maxFileSize)
	if err != nil {
		return nil, err
	}
	defer rc.Close() //nolint:errcheck

	var calls []ToolCall
	err = parser.ScanJSONLLines(rc, func(line []byte) error {
		var env scanEntry
		if err := json.Unmarshal(line, &env); err != nil {
			return nil
		}
		if env.Type != "assistant" && env.Type != "message" {
			return nil
		}
		var ts time.Time
		if env.Timestamp != "" {
			if parsed, perr := time.Parse(time.RFC3339Nano, env.Timestamp); perr == nil {
				ts = parsed
			}
		}
		if !ts.IsZero() {
			if !from.IsZero() && ts.Before(from) {
				return nil
			}
			if !to.IsZero() && ts.After(to) {
				return nil
			}
		}
		var msg scanMessage
		if err := json.Unmarshal(env.Message, &msg); err != nil {
			return nil
		}
		if msg.Role != "assistant" {
			return nil
		}
		for _, raw := range msg.Content {
			var block scanBlock
			if err := json.Unmarshal(raw, &block); err != nil {
				continue
			}
			if block.Type != "tool_use" || !ToolNameRE.MatchString(block.Name) {
				continue
			}
			calls = append(calls, ToolCall{
				SessionID: sessionID,
				Name:      block.Name,
				ID:        block.ID,
				Timestamp: ts,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return calls, nil
}

// scanEntry / scanMessage / scanBlock are private decode targets so we do
// not depend on parser internals or accidentally couple the visualization
// scan to the agent-monitoring tail parse.
type scanEntry struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Message   json.RawMessage `json:"message"`
}

type scanMessage struct {
	Role    string            `json:"role"`
	Content []json.RawMessage `json:"content"`
}

type scanBlock struct {
	Type string `json:"type"`
	Name string `json:"name"`
	ID   string `json:"id"`
}
