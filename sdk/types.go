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

// SubAgent represents a sub-agent spawned by a parent Claude session.
type SubAgent struct {
	ID            string `json:"id"`
	Type          string `json:"type"`
	Status        string `json:"status"` // "active" | "completed"
	CurrentAction string `json:"currentAction"`
	SessionFile   string `json:"sessionFile"`
}

// TaskInfo is a task tracked by Claude Code's internal task list.
type TaskInfo struct {
	ID      string `json:"id"`
	Subject string `json:"subject"`
	Status  string `json:"status"`
}

// AgentStatus is the computed activity state of an agent process.
type AgentStatus string

const (
	AgentStatusActive  AgentStatus = "active"
	AgentStatusWaiting AgentStatus = "waiting"
	AgentStatusIdle    AgentStatus = "idle"
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

// Agent is the unified view of a running Claude Code process.
type Agent struct {
	PID                       int            `json:"pid"`
	SessionID                 string         `json:"sessionId"`
	Provider                  Provider       `json:"provider"`
	ProjectPath               string         `json:"projectPath"`
	ProjectName               string         `json:"projectName"`
	CWD                       string         `json:"cwd"`
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
	// LiveInjectable is true when the dashboard can deliver a prompt to this
	// running interactive session as real keyboard input — either via the pty
	// broker (`agent-dashboard ptyhost`) or `tmux send-keys`. When false, sending
	// resumes the session as a new one (MCP log delivery does not drive it).
	LiveInjectable            bool           `json:"liveInjectable,omitempty"`
	LastOutput                *string        `json:"lastOutput"`
	ConvergenceAlert          bool           `json:"convergenceAlert"`
	ConvergenceToolName       *string        `json:"convergenceToolName"`
	ErrorState                *ErrorState    `json:"errorState"`
	PipelineTaskID            string         `json:"pipelineTaskId,omitempty"`
	PipelineTaskTitle         string         `json:"pipelineTaskTitle,omitempty"`
	Machine                   string         `json:"machine,omitempty"`
	LastBtw                   *BtwMessage    `json:"lastBtw"`
	// CostUnknown is true when the provider does not expose token counts and
	// cost cannot be estimated. CostEstimate will be 0 in this case.
	CostUnknown bool `json:"costUnknown,omitempty"`
}
