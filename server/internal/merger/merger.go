// Package merger combines process scan results with JSONL session data into Agent values.
package merger

import (
	"context"
	"path/filepath"
	"time"

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

	agents := make([]sdk.Agent, 0, len(processes))
	for _, proc := range processes {
		session, err := parser.FindSessionForProject(proc.CWD, proc.Uptime, proc.ClaudeConfigDir)
		if err != nil {
			continue
		}

		cost := parser.EstimateCost(session.TokenUsage, session.Model)
		cacheCreate := parser.EstimateCacheCreationCost(session.TokenUsage, session.Model)
		cacheRead := parser.EstimateCacheReadCost(session.TokenUsage, session.Model)

		agent := sdk.Agent{
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
		agents = append(agents, agent)
	}
	return agents, nil
}
