// Package merger combines process scan results with JSONL session data into Agent values.
package merger

import (
	"context"
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

// channelDiscoveryExists reports whether the dashboard-channel bridge has written
// a discovery file for the given process PID — i.e. the agent carries the
// dashboard-channel MCP and can be messaged live via SendMessageToChannel.
// The file is named by the agent's (parent) PID under ~/.claude/dashboard-channel/.
// Since buildAgent only runs for processes the scanner found alive, a present
// discovery file for that PID means a live, channel-reachable agent.
func channelDiscoveryExists(pid int) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	path := filepath.Join(home, channelconfig.DiscoveryDir, strconv.Itoa(pid)+".json")
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
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

// GetAgents scans running Claude processes and merges them with session data.
// Processes with no matching active session are silently skipped.
func GetAgents(ctx context.Context) ([]sdk.Agent, error) {
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
				agents[i] = buildAgent(proc, session)
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
func buildAgent(proc scanner.ProcessInfo, session *parser.SessionData) sdk.Agent {
	provider := proc.Provider
	if provider == "" {
		provider = sdk.ProviderClaude
	}
	c := EstimateCostForProvider(provider, session.TokenUsage, session.Model)

	return sdk.Agent{
		PID:                       proc.PID,
		SessionID:                 session.SessionID,
		Provider:                  provider,
		ProjectPath:               proc.CWD,
		ProjectName:               filepath.Base(proc.CWD),
		CWD:                       proc.CWD,
		Entrypoint:                session.Entrypoint,
		Status:                    CalculateStatus(session.LastActivity),
		ChannelAvailable:          channelDiscoveryExists(proc.PID),
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
