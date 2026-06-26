package pipeline

import (
	"fmt"
	"maps"
	"strings"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/llmadapter"
)

// SpawnFunc launches the agent process for native (claude) stages.
type SpawnFunc func(opts SpawnAgentOptions) (SpawnResult, error)

// agentStageHandler is the generic stage handler for agent-driven stages.
type agentStageHandler struct {
	stage       string
	buildPrompt func(ctx *StageContext) PromptBundle
	spawnFn     SpawnFunc
}

func (h *agentStageHandler) Stage() string       { return h.stage }
func (h *agentStageHandler) RequiresAgent() bool { return true }

func (h *agentStageHandler) Execute(ctx *StageContext) (StageTransition, error) {
	bundle := h.buildPrompt(ctx)
	// Prepend any custom system prompts from DB (highest priority first).
	if custom := buildCustomSystemPrompt(ctx, h.stage); custom != "" {
		bundle.SystemPrompt = custom + "\n\n---\n\n" + bundle.SystemPrompt
	}
	feedback := BuildFeedbackPrefix(ctx.PriorIterationOutput)
	fullUserPrompt := buildStageUserPrompt(ctx, bundle, feedback)

	// Resolve the effective DB spawner immediately before exec. Failure to
	// resolve is fatal — we do NOT silently fall back to the bare `claude`
	// binary when the task or its project named a spawner that could not be
	// loaded (e.g. it was deleted out from under us). Propagate the error.
	var resolved *ent.Spawner
	if ctx.ResolveSpawner != nil {
		sp, err := ctx.ResolveSpawner(ctx.Ctx, ctx.Task.ID, h.stage)
		if err != nil {
			return nil, fmt.Errorf("agentStageHandler.Execute(%s): resolve spawner: %w", h.stage, err)
		}
		resolved = sp
	}

	// LLM-adapter path: when the resolved row declares a non-Claude adapter
	// type, build the corresponding LLMSpawner and dispatch via the HTTP/
	// custom-command abstraction. The adapter writes its own synthetic JSONL
	// session file so the completion detector can read the output unchanged.
	if resolved != nil && resolved.AdapterType != "" && resolved.AdapterType != "claude" {
		adapter, err := llmadapter.NewLLMSpawnerFromSpawner(resolved)
		if err != nil {
			return nil, fmt.Errorf("agentStageHandler.Execute(%s): build adapter: %w", h.stage, err)
		}
		if adapter == nil {
			// Defensive: factory returned nil for a non-claude type, which
			// should never happen unless the catalog and factory drift apart.
			return nil, fmt.Errorf("agentStageHandler.Execute(%s): adapter factory returned nil for adapter_type %q", h.stage, resolved.AdapterType)
		}

		// Precedence (highest wins): spawner.ModelOverride > task.Metadata["model"]
		// > per-stage config/default. Per-stage default is only the floor.
		model := ""
		if ctx.StageModelFn != nil {
			model = ctx.StageModelFn(ctx.Ctx, h.stage, ctx.Task.ProjectID)
		}
		if ctx.Task.Metadata != nil {
			if m, ok := ctx.Task.Metadata["model"].(string); ok && m != "" {
				model = m
			}
		}
		// Per-spawner ModelOverride wins over everything — mirrors native-path.
		if resolved.ModelOverride != nil && *resolved.ModelOverride != "" {
			model = *resolved.ModelOverride
		}
		cwd := ctx.Task.Cwd
		if ctx.Task.WorktreePath != nil && *ctx.Task.WorktreePath != "" {
			cwd = *ctx.Task.WorktreePath
		}
		allowedTools := buildAllowedToolsList(ctx)
		spawnArgs := llmadapter.LLMSpawnArgs{
			TaskID:       ctx.Task.ID,
			StageRunID:   ctx.StageRun.ID,
			Stage:        h.stage,
			SystemPrompt: bundle.SystemPrompt,
			UserPrompt:   fullUserPrompt,
			Model:        model,
			WorkDir:      cwd,
			AllowedTools: allowedTools,
		}
		stageRunID := ctx.StageRun.ID
		taskID := ctx.Task.ID
		spawnCtx := ctx.Ctx

		ctx.RecordAudit(h.stage+"_dispatched", map[string]any{
			"spawner":     adapter.Name(),
			"adapterType": resolved.AdapterType,
			"iteration":   ctx.StageRun.Iteration,
			"hasFeedback": len(feedback) > 0,
		})

		if ctx.DispatchHTTPSpawn != nil {
			// Async path: dispatch to goroutine pool and return immediately.
			ctx.DispatchHTTPSpawn(stageRunID, taskID, func() (string, error) {
				result, err := adapter.Spawn(spawnCtx, spawnArgs)
				if err != nil {
					return "", fmt.Errorf("spawner %s: %w", adapter.Name(), err)
				}
				return result.SessionFile, nil
			})
			return AsyncRunningTransition{PID: 0}, nil
		}

		// Synchronous fallback for tests or environments without a live orchestrator.
		llmResult, err := adapter.Spawn(spawnCtx, spawnArgs)
		if err != nil {
			return nil, fmt.Errorf("agentStageHandler.Execute(%s): spawner %s: %w", h.stage, adapter.Name(), err)
		}
		return AsyncRunningTransition{PID: llmResult.PID, SessionID: llmResult.SessionID, SessionFile: llmResult.SessionFile}, nil
	}

	// Native Claude path: resolved is either nil, claude-default, or any row
	// with AdapterType=="claude" / "". Behaviour is byte-identical to the
	// pre-adapter-merge code path.
	if h.spawnFn == nil {
		return nil, fmt.Errorf("agentStageHandler.Execute(%s): spawnFn not set", h.stage)
	}

	// Resolve the native-path model following full precedence:
	// per-stage default → task.Metadata["model"] → BUT spawner.ModelOverride
	// must still win. Since BuildSpawnArgs only inserts spawner.ModelOverride
	// when opts.Model is empty, we must NOT set opts.Model when the spawner
	// declares its own override — otherwise opts.Model would silently win.
	nativeModel := ""
	spawnerHasOverride := resolved != nil && resolved.ModelOverride != nil && *resolved.ModelOverride != ""
	if !spawnerHasOverride {
		// Start with per-stage default (coded + DB row), then let task override it.
		if ctx.StageModelFn != nil {
			nativeModel = ctx.StageModelFn(ctx.Ctx, h.stage, ctx.Task.ProjectID)
		}
		if ctx.Task.Metadata != nil {
			if m, ok := ctx.Task.Metadata["model"].(string); ok && m != "" {
				nativeModel = m
			}
		}
	}

	result, err := h.spawnFn(SpawnAgentOptions{
		Task:            ctx.Task,
		StageRun:        ctx.StageRun,
		SystemPrompt:    bundle.SystemPrompt,
		Prompt:          fullUserPrompt,
		Permissions:     ctx.Permissions,
		EnableChannel:   true,
		ResumeSessionID: ctx.ResumeSessionID,
		MCPToken:        ctx.MCPToken,
		MCPUrl:          ctx.MCPUrl,
		Spawner:         resolved,
		Model:           nativeModel,
		AdditionalDirs:  ctx.AdditionalDirs,
		AllowGitPush:    ctx.AllowGitPush,
	})
	if err != nil {
		return nil, fmt.Errorf("agentStageHandler.Execute(%s): %w", h.stage, err)
	}

	ctx.RecordAudit(h.stage+"_spawned", map[string]any{
		"pid":              result.PID,
		"iteration":        ctx.StageRun.Iteration,
		"hasFeedback":      len(feedback) > 0,
		"resumedSessionId": ctx.ResumeSessionID,
	})

	return AsyncRunningTransition{PID: result.PID}, nil
}

// buildAllowedToolsList returns the allow-list strings for the given context,
// mirroring the logic in BuildAllowList so non-Claude adapters receive the
// same tool constraints.
func buildAllowedToolsList(ctx *StageContext) []string {
	raw := BuildAllowList(ctx.Task.Autonomy, ctx.Permissions, false, ctx.AllowGitPush)
	// Strip the Bash(...) wrapper — non-Claude adapters receive plain tool names.
	tools := make([]string, 0, len(raw))
	for _, entry := range raw {
		if strings.HasPrefix(entry, "Bash(") {
			tools = append(tools, "Bash")
			continue
		}
		// Strip pattern suffix, e.g. "Read(*.go)" → "Read"
		if idx := strings.IndexByte(entry, '('); idx >= 0 {
			tools = append(tools, entry[:idx])
			continue
		}
		tools = append(tools, entry)
	}
	return tools
}

// resumeContinueInstruction replaces the full task spec on resume spawns to
// avoid re-sending the entire spec that the session already has in context.
const resumeContinueInstruction = "Continue your previous attempt on this task. Your earlier session context is already loaded — do not restart from scratch or re-read the full specification. Review where you left off, fix what caused the previous failure, and complete the remaining work."

// buildStageUserPrompt assembles the user-facing prompt for a stage execution.
// On resume, the full task spec (bundle.UserPrompt) is swapped for a short
// "continue" instruction; feedback and additional-prompt suffix are preserved.
func buildStageUserPrompt(ctx *StageContext, bundle PromptBundle, feedback string) string {
	userPrompt := bundle.UserPrompt
	if ctx.ResumeSessionID != "" {
		userPrompt = resumeContinueInstruction
	}
	return feedback + userPrompt + buildAdditionalPromptSuffix(ctx.UserAdditionalPrompt)
}

func buildAdditionalPromptSuffix(prompt string) string {
	if prompt == "" {
		return ""
	}
	return fmt.Sprintf("\n\n---\nAdditional instruction from user: %s", prompt)
}

// createAgentStage returns an agent-driven StageHandler for the given stage.
func createAgentStage(stage string, buildPrompt func(*StageContext) PromptBundle, spawnFn SpawnFunc) StageHandler {
	return &agentStageHandler{
		stage:       stage,
		buildPrompt: buildPrompt,
		spawnFn:     spawnFn,
	}
}

type staticHandler struct {
	stage     string
	executeFn func(ctx *StageContext) (StageTransition, error)
}

func (h *staticHandler) Stage() string       { return h.stage }
func (h *staticHandler) RequiresAgent() bool { return false }
func (h *staticHandler) Execute(ctx *StageContext) (StageTransition, error) {
	return h.executeFn(ctx)
}

var conceptHandler StageHandler = &staticHandler{
	stage: "concept",
	executeFn: func(ctx *StageContext) (StageTransition, error) {
		ctx.RecordAudit("concept_chat_pending", nil)
		return WaitUserTransition{Reason: "Refinement chat in progress"}, nil
	},
}

var backlogHandler StageHandler = &staticHandler{
	stage: "backlog",
	executeFn: func(ctx *StageContext) (StageTransition, error) {
		ctx.RecordAudit("backlog_entered", nil)
		return NextTransition{Stage: "implementation"}, nil
	},
}

func implementationBuilder(ctx *StageContext) PromptBundle {
	feedback := ""
	if ctx.Task.Metadata != nil {
		if fb, ok := ctx.Task.Metadata["review_feedback"].(string); ok {
			feedback = fb
		}
	}
	conceptOutput := map[string]any{}
	maps.Copy(conceptOutput, ctx.Task.Metadata)
	return ImplementationPrompt(ctx.Task, conceptOutput, feedback, ctx.AllowGitPush)
}

func selfReviewBuilder(ctx *StageContext) PromptBundle {
	return SelfReviewPrompt(ctx.Task, ctx.PreviousOutput)
}

func finalizationBuilder(ctx *StageContext) PromptBundle {
	return FinalizationPrompt(ctx.Task, ctx.AllStageRuns)
}

func planReviewBuilder(ctx *StageContext) PromptBundle {
	feedback := ""
	if ctx.Task.Metadata != nil {
		if fb, ok := ctx.Task.Metadata["planReviewFeedback"].(string); ok {
			feedback = fb
		}
	}
	conceptOutput := map[string]any{}
	maps.Copy(conceptOutput, ctx.Task.Metadata)
	return PlanReviewPrompt(ctx.Task, conceptOutput, feedback)
}

// NewStageHandlers builds a stage registry with the given spawn function wired
// into all agent-driven stages.
func NewStageHandlers(spawnFn SpawnFunc) map[string]StageHandler {
	return map[string]StageHandler{
		"concept":        conceptHandler,
		"backlog":        backlogHandler,
		"implementation": createAgentStage("implementation", implementationBuilder, spawnFn),
		"self_review":    createAgentStage("self_review", selfReviewBuilder, spawnFn),
		"plan_review":    createAgentStage("plan_review", planReviewBuilder, spawnFn),
		"finalization":   createAgentStage("finalization", finalizationBuilder, spawnFn),
	}
}

// HandlersByStage is the default package-level registry. Agent stages use
// syntheticSpawn so this registry is safe for any code that does not wire a
// real orchestrator (tests, static analysis, etc.).
var HandlersByStage = NewStageHandlers(syntheticSpawn)

// GetHandlerForStage returns the handler for stage, or nil if unregistered.
func GetHandlerForStage(stage string) StageHandler {
	return HandlersByStage[stage]
}
