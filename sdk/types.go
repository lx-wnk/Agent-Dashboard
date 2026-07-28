// Package sdk provides shared types for the agent-dashboard modules.
package sdk

//go:generate tygo generate --config ../tygo.yaml

// TokenUsage mirrors the Claude Code session token counters.
type TokenUsage struct {
	InputTokens         int `json:"inputTokens"`
	OutputTokens        int `json:"outputTokens"`
	CacheCreationTokens int `json:"cacheCreationTokens"`
	CacheReadTokens     int `json:"cacheReadTokens"`
}

// SessionMeta is read from ~/.claude/usage-data/session-meta/{sessionId}.json
type SessionMeta struct {
	InputTokens   int    `json:"inputTokens"`
	OutputTokens  int    `json:"outputTokens"`
	LinesAdded    int    `json:"linesAdded"`
	LinesRemoved  int    `json:"linesRemoved"`
	FilesModified int    `json:"filesModified"`
	GitCommits    int    `json:"gitCommits"`
	ToolErrors    int    `json:"toolErrors"`
	UsesMCP       bool   `json:"usesMcp"`
	FirstPrompt   string `json:"firstPrompt"`
}

// SubAgentStatus is the lifecycle state of a spawned sub-agent.
type SubAgentStatus string

const (
	SubAgentStatusActive    SubAgentStatus = "active"
	SubAgentStatusCompleted SubAgentStatus = "completed"
)

// SubAgent represents a sub-agent spawned by a parent Claude session.
type SubAgent struct {
	ID              string         `json:"id"`
	Type            string         `json:"type"`
	Status          SubAgentStatus `json:"status"`
	CurrentAction   string         `json:"currentAction"`
	SessionFile     string         `json:"sessionFile"`
	TokensUsed      int            `json:"tokensUsed"`
	DurationSeconds int            `json:"durationSeconds"`
	LatestOutput    string         `json:"latestOutput"`
}

// TaskInfoStatus is the state of a TodoWrite-tracked task.
type TaskInfoStatus string

const (
	TaskInfoStatusPending    TaskInfoStatus = "pending"
	TaskInfoStatusInProgress TaskInfoStatus = "in_progress"
	TaskInfoStatusCompleted  TaskInfoStatus = "completed"
)

// TaskInfo is a task tracked by Claude Code's internal task list.
type TaskInfo struct {
	ID      string         `json:"id"`
	Subject string         `json:"subject"`
	Status  TaskInfoStatus `json:"status"`
}

// AgentStatus is the computed activity state of an agent process.
type AgentStatus string

const (
	AgentStatusActive   AgentStatus = "active"
	AgentStatusWaiting  AgentStatus = "waiting"
	AgentStatusIdle     AgentStatus = "idle"
	AgentStatusFinished AgentStatus = "finished"
)

// Entrypoint describes how the Claude Code process was launched.
type Entrypoint string

const (
	EntrypointCLI     Entrypoint = "cli"
	EntrypointDesktop Entrypoint = "desktop"
	EntrypointUnknown Entrypoint = "unknown"
)

// Provider identifies which AI coding CLI an agent process belongs to.
type Provider string

const (
	ProviderClaude Provider = "claude"
	ProviderCodex  Provider = "codex"
	ProviderGemini Provider = "gemini"
	ProviderJunie  Provider = "junie"
)

// ErrorState describes a recognisable error condition seen in the session log.
type ErrorState string

const (
	ErrorStateQuotaExhausted ErrorState = "quota_exhausted"
	ErrorStateRateLimited    ErrorState = "rate_limited"
	ErrorStateAuthFailed     ErrorState = "auth_failed"
)

// SankeyNode is one node in the tool-call Sankey diagram. ID equals Name
// today but is kept separate so collisions across distinct sources can
// later be disambiguated.
type SankeyNode struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SankeyLink is one directed source→target flow with an aggregated count.
type SankeyLink struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Value  int    `json:"value"`
}

// SankeyMeta carries summary counters for the Sankey response.
type SankeyMeta struct {
	SessionCount int `json:"sessionCount"`
	CallCount    int `json:"callCount"`
}

// SankeyData is the response payload for GET /api/visualizations/sankey.
type SankeyData struct {
	Nodes []SankeyNode `json:"nodes"`
	Links []SankeyLink `json:"links"`
	Meta  SankeyMeta   `json:"meta"`
}

// DAGNode is one node in the session DAG (tool call, assistant turn, or
// user message). The DAG is per-session so IDs are line-local.
type DAGNode struct {
	ID    string `json:"id"`
	Type  string `json:"type"` // "tool" | "assistant" | "user"
	Label string `json:"label"`
	Ts    string `json:"ts"` // RFC3339 timestamp, empty if absent
}

// DAGLink is one edge in the session DAG. Kind is "chrono" for time-
// ordered succession or "result" for a tool_use → tool_result match.
type DAGLink struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Kind   string `json:"kind"`
}

// DAGData is the response payload for GET /api/visualizations/dag.
type DAGData struct {
	Nodes []DAGNode `json:"nodes"`
	Links []DAGLink `json:"links"`
}

// SpawnTreeNode is one session node in the spawn tree. Depth is computed
// from the root via BFS. ToolCount and CostCents reflect the session,
// not the cumulative subtree.
type SpawnTreeNode struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Depth       int    `json:"depth"`
	ToolCount   int    `json:"toolCount"`
	CostCents   int    `json:"costCents"`
	Project     string `json:"project"`
	Model       string `json:"model"`
	FirstPrompt string `json:"firstPrompt"`
}

// SpawnTreeLink is a parent→child spawn relationship.
type SpawnTreeLink struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

// SpawnTreeData is the response payload for GET /api/visualizations/spawn-tree.
type SpawnTreeData struct {
	Roots []string        `json:"roots"`
	Nodes []SpawnTreeNode `json:"nodes"`
	Links []SpawnTreeLink `json:"links"`
}

// CoOccurrenceMeta carries summary counters for the co-occurrence response.
type CoOccurrenceMeta struct {
	SessionCount int  `json:"sessionCount"`
	Truncated    bool `json:"truncated"`
}

// CoOccurrenceData is the response payload for
// GET /api/visualizations/co-occurrence. Matrix is square with side equal
// to len(Tools); Matrix[i][j] = sessions containing both Tools[i] and
// Tools[j] (diagonal = sessions containing the tool at all).
// Lift is the same-dimension normalized lift score: Lift[i][j] =
// (Matrix[i][j] × N) / (Matrix[i][i] × Matrix[j][j]), where N is the
// total session count. Diagonal is 0 (self-lift is meaningless).
type CoOccurrenceData struct {
	Tools  []string         `json:"tools"`
	Matrix [][]int          `json:"matrix"`
	Lift   [][]float64      `json:"lift"`
	Meta   CoOccurrenceMeta `json:"meta"`
}

// BtwMessage is the last assistant text that appeared alongside tool calls.
// Message is the text content; Response is reserved for future use.
type BtwMessage struct {
	Message  string  `json:"message"`
	Response *string `json:"response"`
}

// WorktreeStatusDTO describes the live git state of a task's worktree.
// Ahead and Behind are pointers so JSON null is preserved when the base
// branch cannot be resolved on `origin` (e.g. local-only base).
type WorktreeStatusDTO struct {
	Branch    string `json:"branch"`
	Ahead     *int   `json:"ahead"`
	Behind    *int   `json:"behind"`
	Dirty     bool   `json:"dirty"`
	FileCount int    `json:"fileCount"`
}

// PendingPermission is a permission request an orchestrated agent is currently
// blocked on, surfaced on the agent so it can be resolved from the roster.
type PendingPermission struct {
	ID          string  `json:"id"`
	Tool        string  `json:"tool"`
	Pattern     *string `json:"pattern"`
	Reason      *string `json:"reason"`
	RequestedAt string  `json:"requestedAt"`
}

// PendingToolUse is the last assistant tool_use block that has no matching
// tool_result yet. It indicates the agent is currently executing or blocked on
// that tool call. Pattern is the command string (Bash), file path (Edit/Write),
// or empty for other tools.
type PendingToolUse struct {
	ID      string `json:"id"`
	Tool    string `json:"tool"`
	Pattern string `json:"pattern"`
}

// HookEvent is one lifecycle-hook event recorded for a session when the opt-in
// hook receiver is installed (POST /api/hooks/event). It adds per-event
// granularity on top of the process/JSONL scan. Tool and Summary are truncated,
// secret-safe projections of the raw hook payload — never the full tool_input or
// tool_response.
type HookEvent struct {
	Type    string `json:"type"`    // hook type, e.g. "PreToolUse" | "PostToolUse" | "Stop"
	Tool    string `json:"tool"`    // tool name, empty for non-tool hooks
	At      string `json:"at"`      // RFC3339 timestamp the event was received
	Summary string `json:"summary"` // truncated, secret-safe payload preview
}

// DetectedOption is a single numbered option row of a parsed AskUserQuestion
// modal.
type DetectedOption struct {
	Index       int    `json:"index"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// DetectedQuestion is a parsed AskUserQuestion modal, detected from a live
// terminal's rendered rows by askq.DetectQuestion.
//
// Invariant (ENFORCED, not merely typical): the real option rows are numbered
// contiguously 1..n, and the two UI-injected meta-rows follow immediately, so
// TypeSomethingIndex == len(Options)+1 and ChatAboutIndex == TypeSomethingIndex+1.
type DetectedQuestion struct {
	// Header is a best-effort title line above the question. Bordered renders
	// carry a real one; borderless renders leave whatever scrolled above the
	// modal here (a prompt echo, the welcome box). Not UI-facing.
	Header             string           `json:"header"`
	Question           string           `json:"question"`
	MultiSelect        bool             `json:"multiSelect"`
	Options            []DetectedOption `json:"options"`
	TypeSomethingIndex int              `json:"typeSomethingIndex"`
	ChatAboutIndex     int              `json:"chatAboutIndex"`
}

// DetectedConfirm is the AskUserQuestion review/submit screen, detected from a
// live terminal's rendered rows by askq.DetectConfirmScreen.
//
// It is the screen a multi-question AskUserQuestion flow lands on after the
// last question has been answered ("Ready to submit your answers?" plus a
// Submit/Cancel choice). It carries no meta-rows, so it is not a
// DetectedQuestion: there is nothing to type and nothing to chat about, only
// two numbered options to pick from.
type DetectedConfirm struct {
	Question string           `json:"question"`
	Options  []DetectedOption `json:"options"`
}

// PendingScreen is whichever interactive AskUserQuestion screen is currently
// open on a session's terminal. At most one field is non-nil; both are nil when
// no such screen is open. Probing for both in one round-trip keeps the scan hot
// path to a single capture per tick.
type PendingScreen struct {
	Question *DetectedQuestion `json:"question,omitempty"`
	Confirm  *DetectedConfirm  `json:"confirm,omitempty"`
}

// Agent is the unified view of a running Claude Code process.
type Agent struct {
	PID         int      `json:"pid"`
	SessionID   string   `json:"sessionId"`
	Provider    Provider `json:"provider"`
	ProjectPath string   `json:"projectPath"`
	ProjectName string   `json:"projectName"`
	CWD         string   `json:"cwd"`
	// ClaudeConfigDir is the value of CLAUDE_CONFIG_DIR detected in the running
	// session's process env (empty when the session uses the default ~/.claude).
	// Lets the dashboard resolve which config root a session's slash commands /
	// skills / plugins are loaded from when enumerating per-session scope.
	ClaudeConfigDir           string         `json:"claudeConfigDir,omitempty"`
	Entrypoint                Entrypoint     `json:"entrypoint"`
	Status                    AgentStatus    `json:"status"`
	Uptime                    int64          `json:"uptime"`
	LastActivity              string         `json:"lastActivity"`
	CurrentAction             *string        `json:"currentAction"`
	LastTools                 []string       `json:"lastTools"`
	Tasks                     []TaskInfo     `json:"tasks"`
	Subagents                 []SubAgent     `json:"subagents"`
	TokenUsage                TokenUsage     `json:"tokenUsage"`
	CostEstimate              float64        `json:"costEstimate"`
	CacheCreationCostEstimate float64        `json:"cacheCreationCostEstimate"`
	CacheReadCostEstimate     float64        `json:"cacheReadCostEstimate"`
	HealthScore               int            `json:"healthScore"`
	Model                     *string        `json:"model"`
	CodeVersion               *string        `json:"codeVersion"`
	ConversationTurns         int            `json:"conversationTurns"`
	ToolCounts                map[string]int `json:"toolCounts"`
	Meta                      *SessionMeta   `json:"meta"`
	ChannelAvailable          bool           `json:"channelAvailable"`
	// Working is true when the agent is actively generating: it owes the next
	// step (open turn — a trailing user message or unmatched tool_use, via
	// parser.SessionData.TurnOpen) OR a live session emitted output within the
	// last few seconds (pty lastOutputAt / tmux window_activity).
	Working bool `json:"working"`
	// LiveInjectable is true when the dashboard can deliver a prompt to this
	// running interactive session as real keyboard input — either via the pty
	// broker (`agent-dashboard ptyhost`) or `tmux send-keys`. When false, sending
	// resumes the session as a new one (MCP log delivery does not drive it).
	LiveInjectable bool `json:"liveInjectable,omitempty"`
	// PendingQuestion is the AskUserQuestion modal currently detected on this
	// session's live terminal (render-sourced, via the pty broker's /question
	// endpoint or a tmux capture-pane snapshot), or nil when no modal is open.
	// Only ever set on injectable sessions.
	PendingQuestion *DetectedQuestion `json:"pendingQuestion,omitempty"`
	// PendingConfirm is the AskUserQuestion review/submit screen currently
	// detected on this session's live terminal, or nil. Mutually exclusive with
	// PendingQuestion: the TUI shows one or the other, never both. Only ever set
	// on injectable sessions.
	PendingConfirm      *DetectedConfirm    `json:"pendingConfirm,omitempty"`
	LastOutput          *string             `json:"lastOutput"`
	ConvergenceAlert    bool                `json:"convergenceAlert"`
	ConvergenceToolName *string             `json:"convergenceToolName"`
	ErrorState          *ErrorState         `json:"errorState"`
	PipelineTaskID      string              `json:"pipelineTaskId,omitempty"`
	PipelineTaskTitle   string              `json:"pipelineTaskTitle,omitempty"`
	PendingPermissions  []PendingPermission `json:"pendingPermissions,omitempty"`
	PendingToolUse      *PendingToolUse     `json:"pendingToolUse,omitempty"`
	Machine             string              `json:"machine,omitempty"`
	LastBtw             *BtwMessage         `json:"lastBtw"`
	// CostUnknown is true when the provider does not expose token counts and
	// cost cannot be estimated. CostEstimate will be 0 in this case.
	CostUnknown bool `json:"costUnknown,omitempty"`
	// CostLocal is true when the session runs a locally-served (Ollama) model,
	// whose cost is $0 rather than unknown. CostEstimate is 0 in this case.
	CostLocal bool `json:"costLocal,omitempty"`
	// RecentHookEvents carries per-event hook granularity for sessions where the
	// opt-in hook receiver holds events. Omitted entirely when no hook is
	// installed, so clients without hooks receive byte-identical payloads.
	RecentHookEvents []HookEvent `json:"recentHookEvents,omitempty"`
}
