package pipeline

import (
	"encoding/json"
	"fmt"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
)

const sharedContext = `You are an agent working inside a structured task pipeline. A human orchestrator will review your output at specific stages. Be concise, actionable, and honest about uncertainty. When you produce structured output, wrap it in a fenced ` + "```json ... ```" + ` block for the orchestrator to parse.`

const upfrontPermissionsDirective = `## Permissions — declare upfront, in bulk (CRITICAL FIRST STEP)

Before any tool call, scan your task description and the work ahead. Build the FULL list of tools you anticipate needing — file ops (Read/Write/Edit/MultiEdit/Glob/Grep/LS), Bash patterns (e.g. ` + "`pnpm test*`" + `, ` + "`pnpm lint*`" + `, ` + "`git commit*`" + `), WebFetch URLs, etc.

Then call the ` + "`request_permission`" + ` MCP tool ONCE with the full ` + "`permissions: [...]`" + ` array. The dashboard auto-resolves any entries already pre-granted on the task — only truly new entries surface as ON HOLD.

NEVER write prose like "please grant me X" — only ` + "`request_permission`" + ` is actionable.`

// ImplementationPrompt builds the system+user prompt for the implementation stage.
func ImplementationPrompt(t *ent.Task, conceptOutput map[string]any, reviewFeedback string) PromptBundle {
	allowGitPush := IsGitPushAllowed(t)
	pushLine := "Commit your work via git when done — but NEVER `git push`; pushing is the user's responsibility."
	if allowGitPush {
		pushLine = "Commit AND push (`git push`) are permitted for this task — push your feature branch when work is complete."
	}
	systemPrompt := fmt.Sprintf("%s\n\nYou are the orchestrator for this task's implementation phase. Use the Task tool to dispatch subagents for parallel work when beneficial. %s\n\n%s",
		sharedContext, pushLine, upfrontPermissionsDirective)

	conceptJSON, _ := json.MarshalIndent(conceptOutput, "", "  ")
	feedbackBlock := ""
	if reviewFeedback != "" {
		feedbackBlock = fmt.Sprintf("\n\n## Review Feedback From Previous Iteration\n%s\n\nAddress this feedback in your next attempt.", reviewFeedback)
	}
	userPrompt := fmt.Sprintf(`## Task: %s

%s

## Concept (spec, plan, toolRequests)
`+"```json\n%s\n```"+`%s

## Your Job: Implement

Work step-by-step through the concept plan. Commit each logical change via git.

When finished, produce a `+"```json```"+` block as your final output:
{"summary": string, "commits": string[], "openItems": string[]}`,
		t.Title,
		strOrEmpty(t.Description),
		string(conceptJSON),
		feedbackBlock,
	)
	return PromptBundle{SystemPrompt: systemPrompt, UserPrompt: userPrompt}
}

// SelfReviewPrompt builds the prompt for the self_review stage.
func SelfReviewPrompt(t *ent.Task, implementationOutput map[string]any) PromptBundle {
	implJSON, _ := json.MarshalIndent(implementationOutput, "", "  ")
	return PromptBundle{
		SystemPrompt: fmt.Sprintf("%s\n\n%s", sharedContext, upfrontPermissionsDirective),
		UserPrompt: fmt.Sprintf(`## Task: %s

%s

## Implementation Output
`+"```json\n%s\n```"+`

## Your Job: Self-Review

Review the implementation against:
1. Original task requirements — are they all met?
2. Security — any injection, XSS, SQL, auth bypass, secrets leaked?
3. Code quality — DRY violations, dead code, missing error handling?
4. Test coverage — are the changes tested?

Respond with a `+"```json```"+` block: {"passed": bool, "findings": [{"severity": "high"|"medium"|"low", "description": string, "file": string|null}], "summary": string}.`,
			t.Title, strOrEmpty(t.Description), string(implJSON)),
	}
}

// FinalizationPrompt builds the prompt for the finalization stage.
func FinalizationPrompt(t *ent.Task, stageRuns []*ent.StageRun) PromptBundle {
	var history string
	for _, r := range stageRuns {
		history += fmt.Sprintf("%s (iter %d): %s\n", r.Stage, r.Iteration, r.Status)
	}
	return PromptBundle{
		SystemPrompt: fmt.Sprintf("%s\n\n%s", sharedContext, upfrontPermissionsDirective),
		UserPrompt: fmt.Sprintf(`## Task: %s

%s

## Stage History
%s

## Your Job: Final Report

Produce a user-facing summary of what was done. Include:
- Short insights or lessons learned
- Known open todos or caveats
- Concrete test steps the user can run to verify the change

Respond with a `+"```json```"+` block: {"summary": string, "insights": string[], "openTodos": string[], "testPlan": string[]}.`,
			t.Title, strOrEmpty(t.Description), history),
	}
}

func strOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// BuildFeedbackPrefix prepends a correction block to a stage's user prompt
// when the previous iteration's output was schema-rejected.
func BuildFeedbackPrefix(priorOutput map[string]any) string {
	if priorOutput == nil {
		return ""
	}
	validationErr, ok := priorOutput["validation_error"].(string)
	if !ok {
		return ""
	}
	const rejectedPreviewChars = 2000
	rejectedBlock := ""
	if rejected, hasRejected := priorOutput["rejected_output"]; hasRejected {
		full, _ := json.MarshalIndent(rejected, "", "  ")
		truncated := string(full)
		if len(truncated) > rejectedPreviewChars {
			truncated = truncated[:rejectedPreviewChars] + fmt.Sprintf("\n… (truncated, %d chars elided)", len(truncated)-rejectedPreviewChars)
		}
		rejectedBlock = fmt.Sprintf("\n\nYour previous response was:\n```json\n%s\n```", truncated)
	}
	return fmt.Sprintf("## CORRECTION REQUIRED\n\nYour previous attempt was rejected with: **%s**.%s\n\nStick EXACTLY to the schema described below. Do not add or rename fields.\n\n---\n\n", validationErr, rejectedBlock)
}

// SummarizeReviewFindings extracts a short actionable feedback string from a self_review output.
func SummarizeReviewFindings(output map[string]any) string {
	summary, _ := output["summary"].(string)
	findings, _ := output["findings"].([]any)
	var lines []string
	for _, f := range findings {
		fm, ok := f.(map[string]any)
		if !ok {
			continue
		}
		severity, _ := fm["severity"].(string)
		description, _ := fm["description"].(string)
		file, _ := fm["file"].(string)
		fileStr := ""
		if file != "" {
			fileStr = fmt.Sprintf(" (%s)", file)
		}
		if severity == "" {
			severity = "ISSUE"
		}
		lines = append(lines, fmt.Sprintf("- [%s] %s%s", severity, description, fileStr))
	}
	result := summary
	if len(lines) > 0 {
		if result != "" {
			result += "\n"
		}
		for i, l := range lines {
			if i > 0 {
				result += "\n"
			}
			result += l
		}
	}
	return result
}
