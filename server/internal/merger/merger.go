// Package merger combines process scan results with JSONL session data into Agent values.
package merger

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/channelconfig"
	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
	"github.com/lx-wnk/agent-dashboard/server/internal/scanner"
)

const (
	activeThreshold  = 30 * time.Second
	waitingThreshold = 5 * time.Minute
)

// CalculateStatus returns the agent status based on time since last activity.
func CalculateStatus(lastActivity time.Time) sdk.AgentStatus {
	age := time.Since(lastActivity)
	switch {
	case age < activeThreshold:
		return sdk.AgentStatusActive
	case age < waitingThreshold:
		return sdk.AgentStatusWaiting
	default:
		return sdk.AgentStatusIdle
	}
}

// channelDiscovery reads the dashboard-channel discovery files for the given PID
// and returns both availability and live-injectability.
//
// Two files are consulted independently (a missing or unreadable file simply
// contributes nothing — no error):
//
//   - {pid}.json     — channel bridge: written by bridge.go. Presence implies
//     channelAvailable. Contains tmuxPane/tmuxSocket for tmux-based injection.
//   - {pid}.pty.json — pty broker: written by ptyhost.go. Presence also implies
//     channelAvailable. Contains ptyInject:true for loopback-HTTP injection.
//
// This two-file model avoids the collision that occurred on the no-tmux path:
// previously ptyhost.go and bridge.go both wrote to {pid}.json, and the bridge
// (booting ~1s after ptyhost) overwrote the ptyInject field, breaking
// liveInjectable detection.
//
// channelAvailable is true when either file exists.
// liveInjectable is true when the bridge file's tmuxPane is non-empty OR the
// pty file's ptyInject field is true.
func channelDiscovery(pid int) (channelAvailable, liveInjectable bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, false
	}
	base := filepath.Join(home, channelconfig.DiscoveryDir, strconv.Itoa(pid))

	// Read bridge file ({pid}.json).
	if data, err := os.ReadFile(base + ".json"); err == nil {
		channelAvailable = true
		var disc struct {
			TmuxPane string `json:"tmuxPane"`
		}
		if json.Unmarshal(data, &disc) == nil && disc.TmuxPane != "" {
			liveInjectable = true
		}
	}

	// Read pty file ({pid}.pty.json).
	if data, err := os.ReadFile(base + ".pty.json"); err == nil {
		channelAvailable = true
		var disc struct {
			PtyInject bool `json:"ptyInject"`
		}
		if json.Unmarshal(data, &disc) == nil && disc.PtyInject {
			liveInjectable = true
		}
	}

	return channelAvailable, liveInjectable
}

// strPtr returns nil if s is empty, otherwise a pointer to s.
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// errorStatePtr returns nil if s is empty, otherwise a pointer to s.
func errorStatePtr(s sdk.ErrorState) *sdk.ErrorState {
	if s == "" {
		return nil
	}
	return &s
}

// CostBreakdown holds estimated costs plus the "unknown" sentinel.
type CostBreakdown struct {
	Total       float64
	CacheCreate float64
	CacheRead   float64
	Unknown     bool
}

// EstimateCostForProvider returns the cost breakdown for a session, gated by
// provider awareness: non-Claude providers without a known pricing entry are
// flagged as Unknown=true with all costs zeroed. Claude sessions always use
// the pricing table (with default-model fallback) — unchanged from prior behaviour.
func EstimateCostForProvider(provider sdk.Provider, usage sdk.TokenUsage, model string) CostBreakdown {
	if provider != "" && provider != sdk.ProviderClaude && !parser.HasPricing(model) {
		return CostBreakdown{Unknown: true}
	}
	return CostBreakdown{
		Total:       parser.EstimateCost(usage, model),
		CacheCreate: parser.EstimateCacheCreationCost(usage, model),
		CacheRead:   parser.EstimateCacheReadCost(usage, model),
	}
}

// Enricher optionally annotates the freshly-built agent slice with data that
// lives outside the filesystem scan (e.g. pipeline task links from SQLite). It
// mutates agents in place by index and MUST be fail-soft: any lookup error
// leaves the affected agent untouched. A nil Enricher disables enrichment.
//
// Defined here (not in db/repo) so merger stays a leaf package with no db
// dependency; the composition root injects a db-backed implementation, mirroring
// the BaselineProvider injection.
type Enricher func(ctx context.Context, agents []sdk.Agent)

// GetAgentsOpts carries optional settings for a single GetAgents call.
type GetAgentsOpts struct {
	// BaselinePerSessionCostUSD is the average per-session cost over the past 7 days,
	// pre-computed by the caller. Zero means "no baseline available" and disables
	// the cost-spike component of the health score (no penalty).
	BaselinePerSessionCostUSD float64
	// Enricher, when non-nil, is invoked once after the agent slice is built and
	// filtered, before return. See the Enricher type doc.
	Enricher Enricher
}

// scanProcessesFn is the process scanner used by GetAgents. It is a package
// var so tests can inject a synthetic process list without spawning real CLIs.
var scanProcessesFn = scanner.ScanProcesses

// GetAgents scans running Claude processes and merges them with session data.
// Processes with no matching active session are silently skipped.
func GetAgents(ctx context.Context, opts GetAgentsOpts) ([]sdk.Agent, error) {
	processes, err := scanProcessesFn(ctx)
	if err != nil {
		return nil, err
	}

	// Pre-allocate the result slice so each goroutine writes to its own index,
	// avoiding a mutex and producing deterministic ordering (same as processes).
	agents := make([]sdk.Agent, len(processes))

	// Group process indices by project directory (encoded cwd + config dir).
	// Resolution must be sequential WITHIN a group so the shared `claimed` set
	// keeps every same-folder agent on a distinct session — the core fix for
	// "all sessions in one folder show the same content". Groups are independent
	// (different directories cannot share a session file), so they run in
	// parallel to preserve throughput on the per-session tail-reads.
	type groupKey struct{ encoded, configDir string }
	groups := make(map[groupKey][]int)
	for i, proc := range processes {
		k := groupKey{parser.EncodePath(proc.CWD), proc.ClaudeConfigDir}
		groups[k] = append(groups[k], i)
	}

	g, _ := errgroup.WithContext(ctx)
	for _, idxs := range groups {
		idxs := idxs
		g.Go(func() error {
			// Resolve youngest process first so a freshly-started agent claims
			// the freshest session in the fallback path; deterministic ordering
			// keeps the UI stable across ticks.
			sort.SliceStable(idxs, func(a, b int) bool {
				return processes[idxs[a]].Uptime < processes[idxs[b]].Uptime
			})
			claimed := make(map[string]bool)
			for _, i := range idxs {
				proc := processes[i]
				session, err := resolveSession(proc, claimed)
				if err != nil {
					continue // no matching session; zero value left at agents[i]
				}
				agents[i] = buildAgent(proc, session, opts.BaselinePerSessionCostUSD)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	// Filter out zero-value entries (processes with no matching session).
	result := agents[:0]
	for _, a := range agents {
		if a.SessionID != "" {
			result = append(result, a)
		}
	}
	if opts.Enricher != nil {
		opts.Enricher(ctx, result)
	}
	return result, nil
}

// resolveSession dispatches session resolution by provider. Claude (and the
// empty default) use the pid-session-aware path; other providers resolve their
// own config-dir JSONL via the parser's non-Claude path.
func resolveSession(proc scanner.ProcessInfo, claimed map[string]bool) (*parser.SessionData, error) {
	if proc.Provider == "" || proc.Provider == sdk.ProviderClaude {
		return parser.ResolveSessionForProcess(parser.SessionRequest{
			CWD:             proc.CWD,
			PID:             proc.PID,
			Command:         proc.Command,
			UptimeSeconds:   proc.Uptime,
			ClaudeConfigDir: proc.ClaudeConfigDir,
		}, claimed)
	}
	return parser.ResolveNonClaudeSession(proc.Provider, proc.CWD, claimed)
}

// buildAgent assembles an sdk.Agent from a scanned process and its resolved
// session data.
func buildAgent(proc scanner.ProcessInfo, session *parser.SessionData, baselineCost float64) sdk.Agent {
	provider := proc.Provider
	if provider == "" {
		provider = sdk.ProviderClaude
	}
	c := EstimateCostForProvider(provider, session.TokenUsage, session.Model)
	chanAvail, chanInject := channelDiscovery(proc.PID)
	health := ComputeHealthScore(session, c.Total, c.Unknown, baselineCost)

	return sdk.Agent{
		PID:                       proc.PID,
		SessionID:                 session.SessionID,
		Provider:                  provider,
		ProjectPath:               proc.CWD,
		ProjectName:               filepath.Base(proc.CWD),
		CWD:                       proc.CWD,
		ClaudeConfigDir:           proc.ClaudeConfigDir,
		Entrypoint:                session.Entrypoint,
		Status:                    CalculateStatus(session.LastActivity),
		ChannelAvailable:          chanAvail,
		LiveInjectable:            chanInject,
		Uptime:                    proc.Uptime,
		LastActivity:              session.LastActivity.Format(time.RFC3339),
		CurrentAction:             strPtr(session.CurrentAction),
		LastTools:                 append(make([]string, 0), session.LastTools...),
		Tasks:                     append(make([]sdk.TaskInfo, 0), session.Tasks...),
		Subagents:                 []sdk.SubAgent{},
		TokenUsage:                session.TokenUsage,
		CostEstimate:              c.Total,
		CacheCreationCostEstimate: c.CacheCreate,
		CacheReadCostEstimate:     c.CacheRead,
		CostUnknown:               c.Unknown,
		HealthScore:               health,
		Model:                     strPtr(session.Model),
		ConversationTurns:         session.ConversationTurns,
		ToolCounts:                session.ToolCounts,
		Meta:                      session.Meta,
		ConvergenceAlert:          session.ConvergenceAlert,
		ConvergenceToolName:       strPtr(session.ConvergenceToolName),
		ErrorState:                errorStatePtr(session.ErrorState),
		LastOutput:                strPtr(session.LastOutput),
		LastBtw:                   session.LastBtw,
		PendingToolUse:            session.PendingToolUse,
	}
}
