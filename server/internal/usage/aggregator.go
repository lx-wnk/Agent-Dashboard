package usage

import (
	"encoding/json"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
	"github.com/lx-wnk/agent-dashboard/server/internal/pricing"
)

const (
	window5h = 5 * time.Hour
	window7d = 7 * 24 * time.Hour
	cacheTTL = 60 * time.Second
)

// sessionFileRE matches Claude session JSONL filenames (UUID + .jsonl).
var sessionFileRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.jsonl$`)

// WindowUsage is the token and cost aggregate for one rolling window.
type WindowUsage struct {
	Tokens    int64
	CostCents int64 // USD cost rounded to cents
}

// Account is one config-dir's usage (one Claude account/identity).
type Account struct {
	Label string
	W5h   WindowUsage
	W7d   WindowUsage
}

// Result is the full aggregation output.
type Result struct {
	W5h      WindowUsage
	W7d      WindowUsage
	Accounts []Account
}

// Options controls aggregator behaviour. All fields are optional.
type Options struct {
	// ConfigDirs returns the Claude config directories to scan.
	// Defaults to parser.AllClaudeConfigDirs.
	ConfigDirs func() []string
	// Now returns the current time. Defaults to time.Now.
	Now func() time.Time
	// OnScan is called each time a real scan executes (not a cache hit).
	// Useful for tests to count scans.
	OnScan func()
}

// Aggregator scans session JSONLs and aggregates token usage per rolling window.
type Aggregator struct {
	opts Options

	mu       sync.Mutex
	cached   *Result
	cachedAt time.Time
}

// NewAggregator constructs an Aggregator with optional overrides.
func NewAggregator(opts Options) *Aggregator {
	if opts.ConfigDirs == nil {
		opts.ConfigDirs = parser.AllClaudeConfigDirs
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.OnScan == nil {
		opts.OnScan = func() {}
	}
	return &Aggregator{opts: opts}
}

// Aggregate returns cached results if within the 60 s TTL, else re-scans.
func (a *Aggregator) Aggregate() (*Result, error) {
	now := a.opts.Now()

	a.mu.Lock()
	if a.cached != nil && now.Sub(a.cachedAt) < cacheTTL {
		r := a.cached
		a.mu.Unlock()
		return r, nil
	}
	a.mu.Unlock()

	a.opts.OnScan()

	res, err := a.scan(now)
	if err != nil {
		return nil, err
	}

	a.mu.Lock()
	a.cached = res
	a.cachedAt = now
	a.mu.Unlock()

	return res, nil
}

func (a *Aggregator) scan(now time.Time) (*Result, error) {
	cutoff7d := now.Add(-window7d)
	cutoff5h := now.Add(-window5h)

	var total Result
	for _, dir := range a.opts.ConfigDirs() {
		acc := Account{Label: filepath.Base(dir)}
		if err := scanConfigDir(dir, now, cutoff7d, cutoff5h, &acc.W5h, &acc.W7d); err != nil {
			slog.Debug("usage: scan dir skipped", "dir", dir, "err", err)
			continue
		}
		total.Accounts = append(total.Accounts, acc)
		total.W5h.Tokens += acc.W5h.Tokens
		total.W5h.CostCents += acc.W5h.CostCents
		total.W7d.Tokens += acc.W7d.Tokens
		total.W7d.CostCents += acc.W7d.CostCents
	}
	return &total, nil
}

func scanConfigDir(configDir string, now, cutoff7d, cutoff5h time.Time, w5h, w7d *WindowUsage) error {
	projectsDir := filepath.Join(configDir, "projects")
	projectDirs, err := os.ReadDir(projectsDir)
	if err != nil {
		return err
	}
	for _, pDir := range projectDirs {
		if !pDir.IsDir() {
			continue
		}
		dirPath := filepath.Join(projectsDir, pDir.Name())
		files, err := os.ReadDir(dirPath)
		if err != nil {
			slog.Debug("usage: skip project dir", "path", dirPath, "err", err)
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
			if info.ModTime().Before(cutoff7d) {
				continue // mtime prefilter: skip files untouched in >7d
			}
			path := filepath.Join(dirPath, name)
			if err := scanJSONLFile(path, now, cutoff7d, cutoff5h, w5h, w7d); err != nil {
				slog.Debug("usage: skip file", "path", path, "err", err)
			}
		}
	}
	return nil
}

// usageEntry is the JSONL line shape we care about.
type usageEntry struct {
	Timestamp string `json:"timestamp"`
	Message   struct {
		Role  string     `json:"role"`
		Model string     `json:"model"`
		Usage *usageCnts `json:"usage"`
	} `json:"message"`
}

type usageCnts struct {
	Input       int `json:"input_tokens"`
	Output      int `json:"output_tokens"`
	CacheCreate int `json:"cache_creation_input_tokens"`
	CacheRead   int `json:"cache_read_input_tokens"`
}

func scanJSONLFile(path string, now, cutoff7d, cutoff5h time.Time, w5h, w7d *WindowUsage) error {
	rc, err := parser.OpenJSONLReader(path, 0) // 0 = read whole file
	if err != nil {
		return err
	}
	defer rc.Close() //nolint:errcheck

	return parser.ScanJSONLLines(rc, func(line []byte) error {
		var e usageEntry
		if err := json.Unmarshal(line, &e); err != nil {
			return nil // malformed line: skip without aborting the scan
		}
		if e.Message.Role != "assistant" || e.Message.Usage == nil {
			return nil
		}

		var ts time.Time
		if e.Timestamp != "" {
			if parsed, err := time.Parse(time.RFC3339Nano, e.Timestamp); err == nil {
				ts = parsed
			}
		}
		if ts.IsZero() {
			return nil // no timestamp: cannot place in window
		}
		if ts.After(now) {
			return nil // future timestamp (clock skew): out of range
		}

		u := e.Message.Usage
		tokens := int64(u.Input + u.Output + u.CacheCreate + u.CacheRead)
		costUSD := pricing.EstimateCost(sdk.TokenUsage{
			InputTokens:         u.Input,
			OutputTokens:        u.Output,
			CacheCreationTokens: u.CacheCreate,
			CacheReadTokens:     u.CacheRead,
		}, e.Message.Model)
		costCents := int64(math.Round(costUSD * 100))

		if !ts.Before(cutoff7d) {
			w7d.Tokens += tokens
			w7d.CostCents += costCents
		}
		if !ts.Before(cutoff5h) {
			w5h.Tokens += tokens
			w5h.CostCents += costCents
		}
		return nil
	})
}
