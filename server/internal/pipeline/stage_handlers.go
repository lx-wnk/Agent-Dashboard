package pipeline

import (
	"fmt"
	"os"
	"strings"
)

// agentStageHandler is the generic stage handler for agent-driven stages.
type agentStageHandler struct {
	stage       string
	buildPrompt func(ctx *StageContext) PromptBundle
	spawnFn     func(opts SpawnAgentOptions) (SpawnResult, error)
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
	fullUserPrompt := feedback + bundle.UserPrompt + buildAdditionalPromptSuffix(ctx.UserAdditionalPrompt)

	// When an alternative LLM adapter is configured, use it instead of the
	// Claude CLI spawner. The adapter writes its own synthetic JSONL session
	// file so the completion detector can read the output unchanged.
	//
	// HTTP adapters block for up to several minutes, so the actual Spawn call
	// is dispatched to a bounded goroutine pool via DispatchHTTPSpawn. The
	// transition returned here has PID=0; drainHTTPResults writes the synthetic
	// session file path back into stage_run.output when the goroutine finishes,
	// and finalizeCompletedAsyncRuns picks it up on the next tick.
	if ctx.Spawner != nil {
		model := ""
		if ctx.Task.Metadata != nil {
			if m, ok := ctx.Task.Metadata["model"].(string); ok {
				model = m
			}
		}
		cwd := ctx.Task.Cwd
		if ctx.Task.WorktreePath != nil && *ctx.Task.WorktreePath != "" {
			cwd = *ctx.Task.WorktreePath
		}
		allowedTools := buildAllowedToolsList(ctx)
		spawnArgs := LLMSpawnArgs{
			TaskID:       ctx.Task.ID,
			StageRunID:   ctx.StageRun.ID,
			Stage:        h.stage,
			SystemPrompt: bundle.SystemPrompt,
			UserPrompt:   fullUserPrompt,
			Model:        model,
			WorkDir:      cwd,
			AllowedTools: allowedTools,
		}
		spawner := ctx.Spawner
		stageRunID := ctx.StageRun.ID
		taskID := ctx.Task.ID
		spawnCtx := ctx.Ctx

		ctx.RecordAudit(h.stage+"_dispatched", map[string]any{
			"spawner":     spawner.Name(),
			"iteration":   ctx.StageRun.Iteration,
			"hasFeedback": len(feedback) > 0,
		})

		if ctx.DispatchHTTPSpawn != nil {
			// Async path: dispatch to goroutine pool and return immediately.
			ctx.DispatchHTTPSpawn(stageRunID, taskID, func() (string, error) {
				result, err := spawner.Spawn(spawnCtx, spawnArgs)
				if err != nil {
					return "", fmt.Errorf("spawner %s: %w", spawner.Name(), err)
				}
				return result.SessionFile, nil
			})
			return AsyncRunningTransition{PID: 0}, nil
		}

		// Synchronous fallback for tests or environments without a live orchestrator.
		llmResult, err := spawner.Spawn(spawnCtx, spawnArgs)
		if err != nil {
			return nil, fmt.Errorf("agentStageHandler.Execute(%s): spawner %s: %w", h.stage, spawner.Name(), err)
		}
		return AsyncRunningTransition{PID: llmResult.PID, SessionID: llmResult.SessionID, SessionFile: llmResult.SessionFile}, nil
	}

	spawnFn := h.spawnFn
	if spawnFn == nil {
		spawnFn = SpawnStageAgent
	}

	result, err := spawnFn(SpawnAgentOptions{
		Task:            ctx.Task,
		StageRun:        ctx.StageRun,
		SystemPrompt:    bundle.SystemPrompt,
		Prompt:          fullUserPrompt,
		Permissions:     ctx.Permissions,
		EnableChannel:   true,
		ResumeSessionID: ctx.ResumeSessionID,
		MCPToken:        ctx.MCPToken,
		MCPUrl:          ctx.MCPUrl,
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
	raw := BuildAllowList(ctx.Permissions, false, isGitPushAllowedFromEnv())
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

func isGitPushAllowedFromEnv() bool {
	return os.Getenv("DASHBOARD_ALLOW_GIT_PUSH") == "true"
}

func buildAdditionalPromptSuffix(prompt string) string {
	if prompt == "" {
		return ""
	}
	return fmt.Sprintf("\n\n---\nAdditional instruction from user: %s", prompt)
}

// createAgentStage returns an agent-driven StageHandler for the given stage.
func createAgentStage(stage string, buildPrompt func(*StageContext) PromptBundle, spawnFn func(SpawnAgentOptions) (SpawnResult, error)) StageHandler {
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
	if ctx.Task.Metadata != nil {
		for k, v := range ctx.Task.Metadata {
			conceptOutput[k] = v
		}
	}
	return ImplementationPrompt(ctx.Task, conceptOutput, feedback)
}

func selfReviewBuilder(ctx *StageContext) PromptBundle {
	return SelfReviewPrompt(ctx.Task, ctx.PreviousOutput)
}

func finalizationBuilder(ctx *StageContext) PromptBundle {
	return FinalizationPrompt(ctx.Task, ctx.AllStageRuns)
}

// HandlersByStage is the registry of all stage handlers.
var HandlersByStage = map[string]StageHandler{
	"concept":        conceptHandler,
	"backlog":        backlogHandler,
	"implementation": createAgentStage("implementation", implementationBuilder, nil),
	"self_review":    createAgentStage("self_review", selfReviewBuilder, nil),
	"finalization":   createAgentStage("finalization", finalizationBuilder, nil),
}

// GetHandlerForStage returns the handler for stage, or nil if unregistered.
func GetHandlerForStage(stage string) StageHandler {
	return HandlersByStage[stage]
}
