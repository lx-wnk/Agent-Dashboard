// Package history provides cost-trend history import from Claude Code JSONL session logs.
package history

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
)

const (
	maxFileSizeBytes   = 100 * 1024 * 1024 // 100 MB
	progressDebounceMs = 100
)

// ImportProgress is the live status snapshot sent to SSE clients.
type ImportProgress struct {
	Total     int  `json:"total"`
	Processed int  `json:"processed"`
	Imported  int  `json:"imported"`
	Errors    int  `json:"errors"`
	Done      bool `json:"done"`
}

// Importer scans CLAUDE_PROJECTS_DIR, extracts per-session cost data, and bulk-inserts
// into the agent_cost_trend table. Only one concurrent run is permitted per instance.
type Importer struct {
	costRepo  repo.AgentCostTrendRepo
	collectFn func(string) ([]string, error) // injectable for tests; nil → collectJSONLFiles
	mu        sync.Mutex
	running   bool
}

// NewImporter creates an Importer backed by costRepo.
func NewImporter(costRepo repo.AgentCostTrendRepo) *Importer {
	return &Importer{costRepo: costRepo}
}

// WithCollectFn returns a shallow copy of imp with fn as the file-collection function.
// For use in tests only — overrides the default collectJSONLFiles scan.
func (imp *Importer) WithCollectFn(fn func(string) ([]string, error)) *Importer {
	return &Importer{costRepo: imp.costRepo, collectFn: fn}
}

// Run starts the import in a background goroutine. Returns immediately.
// Returns an error if an import is already in progress.
// onProgress is called after each file is processed and when the run completes.
func (imp *Importer) Run(ctx context.Context, onProgress func(ImportProgress)) error {
	imp.mu.Lock()
	if imp.running {
		imp.mu.Unlock()
		return fmt.Errorf("import already in progress")
	}
	imp.running = true
	imp.mu.Unlock()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("history.import: panic in import goroutine", "panic", r)
				onProgress(ImportProgress{Done: true})
			}
			imp.mu.Lock()
			imp.running = false
			imp.mu.Unlock()
		}()
		imp.runImport(ctx, onProgress)
	}()
	return nil
}

// runImport performs the full scan + insert and reports progress via onProgress.
func (imp *Importer) runImport(ctx context.Context, onProgress func(ImportProgress)) {
	projectsDir := parser.ClaudeProjectsDir()

	collect := imp.collectFn
	if collect == nil {
		collect = collectJSONLFiles
	}
	// Collect all JSONL files one level deep: projects/{encoded_path}/*.jsonl
	files, err := collect(projectsDir)
	if err != nil {
		slog.Warn("history.import: failed to collect jsonl files", "err", err)
		onProgress(ImportProgress{Done: true})
		return
	}

	progress := ImportProgress{Total: len(files)}
	onProgress(progress)

	// Debounce — report at most once per progressDebounceMs, but always after each file.
	var (
		lastReport = time.Now()
		debounce   = time.Duration(progressDebounceMs) * time.Millisecond
	)
	reportProgress := func(final bool) {
		now := time.Now()
		if final || now.Sub(lastReport) >= debounce {
			onProgress(progress)
			lastReport = now
		}
	}

	var rows []repo.AgentCostRow

	for _, filePath := range files {
		if ctx.Err() != nil {
			break
		}

		row, err := extractCostRow(filePath)
		if err != nil {
			slog.Debug("history.import: skip file", "path", filePath, "err", err)
			progress.Errors++
			progress.Processed++
			reportProgress(false)
			continue
		}
		if row != nil {
			rows = append(rows, *row)
			progress.Imported++
		}
		progress.Processed++
		reportProgress(false)
	}

	// Bulk-insert all collected rows.
	if len(rows) > 0 {
		if err := imp.costRepo.BulkInsert(ctx, rows); err != nil {
			slog.Warn("history.import: bulk insert failed", "err", err)
			// Whole batch failed — reflect that in error count.
			progress.Errors += progress.Imported
			progress.Imported = 0
		}
	}

	progress.Done = true
	reportProgress(true)
}

// collectJSONLFiles returns all *.jsonl paths one level deep inside projectsDir.
func collectJSONLFiles(projectsDir string) ([]string, error) {
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // nothing to import
		}
		return nil, fmt.Errorf("readdir %s: %w", projectsDir, err)
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		subDir := filepath.Join(projectsDir, entry.Name())
		subEntries, err := os.ReadDir(subDir)
		if err != nil {
			continue
		}
		for _, sub := range subEntries {
			if sub.IsDir() || !strings.HasSuffix(sub.Name(), ".jsonl") {
				continue
			}
			info, err := sub.Info()
			if err != nil {
				continue
			}
			if info.Size() > maxFileSizeBytes {
				continue // skip files larger than 100 MB
			}
			files = append(files, filepath.Join(subDir, sub.Name()))
		}
	}
	return files, nil
}

// extractCostRow tail-reads filePath, parses JSONL, and returns an AgentCostRow.
// Returns (nil, nil) when the file has no usable token data.
func extractCostRow(filePath string) (*repo.AgentCostRow, error) {
	raw, err := parser.TailRead(filePath)
	if err != nil {
		return nil, fmt.Errorf("tailread: %w", err)
	}

	sessionID := sessionIDFromPath(filePath)
	usage, model, lastActivity, err := parseTokensFromRaw(raw)
	if err != nil {
		return nil, err
	}
	if usage.InputTokens == 0 && usage.OutputTokens == 0 {
		return nil, nil // no usable data
	}

	cost := parser.EstimateCost(usage, model)
	return &repo.AgentCostRow{
		SessionID:    sessionID,
		Model:        model,
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		CostUSD:      cost,
		RecordedAt:   lastActivity,
	}, nil
}

// sessionIDFromPath extracts the session UUID from a JSONL filename.
func sessionIDFromPath(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, ".jsonl")
}

// jsonlEntry is the minimal structure of a JSONL session log entry.
type jsonlEntry struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		Role  string `json:"role"`
		Model string `json:"model"`
		Usage *struct {
			InputTokens         int `json:"input_tokens"`
			OutputTokens        int `json:"output_tokens"`
			CacheCreationTokens int `json:"cache_creation_input_tokens"`
			CacheReadTokens     int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// parseTokensFromRaw extracts cumulative token usage, model, and last activity from raw JSONL.
func parseTokensFromRaw(raw string) (sdk.TokenUsage, string, time.Time, error) {
	var (
		total        sdk.TokenUsage
		model        = "claude-sonnet-4-6"
		lastActivity = time.Now().Add(-24 * time.Hour)
	)

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e jsonlEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		if e.Type != "message" || e.Message.Role != "assistant" {
			continue
		}
		if e.Message.Model != "" {
			model = e.Message.Model
		}
		if e.Message.Usage != nil {
			total.InputTokens += e.Message.Usage.InputTokens
			total.OutputTokens += e.Message.Usage.OutputTokens
			total.CacheCreationTokens += e.Message.Usage.CacheCreationTokens
			total.CacheReadTokens += e.Message.Usage.CacheReadTokens
		}
		if ts, err := time.Parse(time.RFC3339Nano, e.Timestamp); err == nil {
			if ts.After(lastActivity) {
				lastActivity = ts
			}
		}
	}

	return total, model, lastActivity, nil
}
