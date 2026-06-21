package merger

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
)

// liveSnapshot caches the minimum a finished agent card needs to be
// reconstructed from JSONL after its process has exited.
type liveSnapshot struct {
	sessionID   string
	path        string
	projectPath string
	configDir   string
	provider    sdk.Provider
}

// staleTracker is an in-memory, process-scoped registry of controllable agents
// (channel/MCP-connected) seen while live, so their cards can stay visible as
// finished after the process exits. channelFn and parseFn are injectable for
// tests; defaults hit the real discovery files and JSONL parser.
type staleTracker struct {
	mu        sync.Mutex
	seen      map[int]liveSnapshot
	channelFn func(pid int) bool
	parseFn   func(path string) (*parser.SessionData, error)
}

func newStaleTracker() *staleTracker {
	return &staleTracker{
		seen:      make(map[int]liveSnapshot),
		channelFn: func(pid int) bool { avail, _ := channelDiscovery(pid); return avail },
		parseFn:   parser.ParseSessionFile,
	}
}

// record stores a snapshot for a live agent. Snapshots without a session id or
// path are useless for later reconstruction and are dropped.
func (t *staleTracker) record(pid int, snap liveSnapshot) {
	if snap.sessionID == "" || snap.path == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.seen[pid] = snap
}

// buildStale emits a finished sdk.Agent for each tracked pid whose process is no
// longer live but whose channel discovery file still exists. A pid whose
// discovery file is gone has been dismissed — forget it so a reused pid cannot
// resurrect a stale card.
func (t *staleTracker) buildStale(livePIDs map[int]bool, baselineCost float64) []sdk.Agent {
	t.mu.Lock()
	defer t.mu.Unlock()

	var out []sdk.Agent
	for pid, snap := range t.seen {
		if livePIDs[pid] {
			continue
		}
		if !t.channelFn(pid) {
			delete(t.seen, pid) // discovery gone => dismissed
			continue
		}
		session, err := t.parseFn(snap.path)
		if err != nil || session == nil || session.SessionID == "" {
			// Transient parse miss; retry next tick. A permanently unparseable
			// dead pid stays in seen for the process lifetime — accepted bounded
			// leak for this process-scoped registry; no age-out by design.
			continue
		}
		out = append(out, buildFinishedAgent(pid, snap, session, baselineCost))
	}
	return out
}

// buildFinishedAgent mirrors buildAgent but reconstructs a finished, controllable
// card from a cached snapshot plus freshly parsed session data. Uptime is left
// zero because the process is gone.
// Keep sdk.Agent field population in parity with buildAgent in merger.go — a new sdk.Agent field must be added to both.
func buildFinishedAgent(pid int, snap liveSnapshot, session *parser.SessionData, baselineCost float64) sdk.Agent {
	provider := snap.provider
	if provider == "" {
		provider = sdk.ProviderClaude
	}
	c := EstimateCostForProvider(provider, session.TokenUsage, session.Model)
	health := ComputeHealthScore(session, c.Total, c.Unknown, baselineCost)

	return sdk.Agent{
		PID:                       pid,
		SessionID:                 session.SessionID,
		Provider:                  provider,
		ProjectPath:               snap.projectPath,
		ProjectName:               filepath.Base(snap.projectPath),
		CWD:                       snap.projectPath,
		ClaudeConfigDir:           snap.configDir,
		Entrypoint:                session.Entrypoint,
		Status:                    sdk.AgentStatusFinished,
		ChannelAvailable:          true,
		LiveInjectable:            false,
		LastActivity:              session.LastActivity.Format(time.RFC3339),
		CurrentAction:             strPtr(session.CurrentAction),
		LastTools:                 append(make([]string, 0), session.LastTools...),
		Tasks:                     append(make([]sdk.TaskInfo, 0), session.Tasks...),
		Subagents:                 buildSubagents(session),
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
