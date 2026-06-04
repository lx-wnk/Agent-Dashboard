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

// channelDiscovery reads the dashboard-channel discovery file for the given PID
// once and returns both availability and live-injectability in a single I/O
// operation, replacing the prior two-call pattern (os.Stat + os.ReadFile).
//
// channelAvailable is true when the file exists (existence implies the agent
// carries the dashboard-channel MCP and can be messaged via SendMessageToChannel).
// liveInjectable is true when the file additionally contains a tmux pane reference
// or pty-inject flag — either of which enables real keyboard-input delivery into
// the running interactive Claude session.
func channelDiscovery(pid int) (channelAvailable, liveInjectable bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, false
	}
	path := filepath.Join(home, channelconfig.DiscoveryDir, strconv.Itoa(pid)+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		// File absent or unreadable — agent has no channel bridge.
		return false, false
	}
	// File present → channel is available.
	channelAvailable = true
	var disc struct {
		TmuxPane  string `json:"tmuxPane"`
		PtyInject bool   `json:"ptyInject"`
	}
	if json.Unmarshal(data, &disc) == nil {
		liveInjectable = disc.TmuxPane != "" || disc.PtyInject
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

// GetAgentsOpts carries optional settings for a single GetAgents call.
type GetAgentsOpts struct {
	// BaselinePerSessionCostUSD is the average per-session cost over the past 7 days,
	// pre-computed by the caller. Zero means "no baseline available" and disables
	// the cost-spike component of the health score (no penalty).
	BaselinePerSessionCostUSD float64
}

// GetAgents scans running Claude processes and merges them with session data.
// Processes with no matching active session are silently skipped.
func GetAgents(ctx context.Context, opts GetAgentsOpts) ([]sdk.Agent, error) {
	processes, err := scanner.ScanProcesses(ctx)
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
				session, err := parser.ResolveSessionForProcess(parser.SessionRequest{
					CWD:             proc.CWD,
					PID:             proc.PID,
					Command:         proc.Command,
					UptimeSeconds:   proc.Uptime,
					ClaudeConfigDir: proc.ClaudeConfigDir,
				}, claimed)
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
	return result, nil
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
	}
}
