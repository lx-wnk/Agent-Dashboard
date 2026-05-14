// Package merger combines process scan results with JSONL session data into Agent values.
package merger

import (
	"context"
	"path/filepath"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/lx-wnk/agent-dashboard/sdk"
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

	g, _ := errgroup.WithContext(ctx)
	for i, proc := range processes {
		i, proc := i, proc
		g.Go(func() error {
			session, err := parser.FindSessionForProject(proc.CWD, proc.Uptime, proc.ClaudeConfigDir)
			if err != nil {
				return nil // skip processes with no matching session; zero value left at agents[i]
			}

			cost := parser.EstimateCost(session.TokenUsage, session.Model)
			cacheCreate := parser.EstimateCacheCreationCost(session.TokenUsage, session.Model)
			cacheRead := parser.EstimateCacheReadCost(session.TokenUsage, session.Model)

			agents[i] = sdk.Agent{
				PID:                       proc.PID,
				SessionID:                 session.SessionID,
				ProjectPath:               proc.CWD,
				ProjectName:               filepath.Base(proc.CWD),
				CWD:                       proc.CWD,
				Entrypoint:                session.Entrypoint,
				Status:                    CalculateStatus(session.LastActivity),
				Uptime:                    proc.Uptime,
				LastActivity:              session.LastActivity.Format(time.RFC3339),
				CurrentAction:             session.CurrentAction,
				LastTools:                 append(make([]string, 0), session.LastTools...),
				Tasks:                     append(make([]sdk.TaskInfo, 0), session.Tasks...),
				Subagents:                 []sdk.SubAgent{},
				TokenUsage:                session.TokenUsage,
				CostEstimate:              cost,
				CacheCreationCostEstimate: cacheCreate,
				CacheReadCostEstimate:     cacheRead,
				Model:                     session.Model,
				ConversationTurns:         session.ConversationTurns,
				ToolCounts:                session.ToolCounts,
				Meta:                      session.Meta,
				ConvergenceAlert:          session.ConvergenceAlert,
				ConvergenceToolName:       session.ConvergenceToolName,
				ErrorState:                session.ErrorState,
				LastOutput:                session.LastOutput,
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
