package db

// Canonical task-creation defaults. Single source of truth for the values
// applied when a create request omits them. The ent schema in
// db/ent/schema/task.go mirrors these literals as codegen-time field defaults
// (it cannot import this package — ent codegen would form an import cycle);
// keep the two in sync.
const (
	DefaultStage               = "concept"
	DefaultPriority            = "medium"
	DefaultMaxIterations       = 20
	DefaultStageTimeoutSeconds = 1800
	DefaultCostBudgetCents     = 500      // $5 per-task cost guardrail
	DefaultTokenBudget         = 15000000 // 15M tokens per-task guardrail
)
