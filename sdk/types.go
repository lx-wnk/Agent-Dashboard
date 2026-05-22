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

// BtwMessage is the last assistant text that appeared alongside tool calls.
// Message is the text content; Response is reserved for future use.
type BtwMessage struct {
	Message  string  `json:"message"`
	Response *string `json:"response"`
}

// Agent is the unified view of a running Claude Code process.
type Agent struct {
	PID                       int            `json:"pid"`
	SessionID                 string         `json:"sessionId"`
	Provider                  Provider       `json:"provider"`
	ProjectPath               string         `json:"projectPath"`
	ProjectName               string         `json:"projectName"`
	CWD                       string         `json:"cwd"`
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
	LastOutput                *string        `json:"lastOutput"`
	ConvergenceAlert          bool           `json:"convergenceAlert"`
	ConvergenceToolName       *string        `json:"convergenceToolName"`
	ErrorState                *ErrorState    `json:"errorState"`
	PipelineTaskID            string         `json:"pipelineTaskId,omitempty"`
	PipelineTaskTitle         string         `json:"pipelineTaskTitle,omitempty"`
	Machine                   string         `json:"machine,omitempty"`
	LastBtw                   *BtwMessage    `json:"lastBtw"`
}
